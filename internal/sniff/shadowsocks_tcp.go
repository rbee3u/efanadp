package sniff

import (
	"encoding/binary"
)

// Shadowsocks AEAD stream framing: a salt, then length-prefixed chunks whose
// 2-byte length and payload each carry their own tag.
const (
	shadowsocksAEADLengthFieldSize = 2
	shadowsocksMaxChunkPayload     = 0x3fff
)

// Shadowsocks 2022 request header: one type byte, an 8-byte timestamp, and the
// 2-byte length of the variable-length header that carries the target address.
const (
	shadowsocks2022HeaderLenOffset   = 9
	shadowsocks2022FixedHeaderLen    = 11
	shadowsocks2022MaxVariableHeader = 0xffff
)

type shadowsocksSniffState int

const (
	sniffNeedMore shadowsocksSniffState = iota
	sniffFound
	sniffFailed
)

// ShadowsocksTCPSniffer incrementally inspects one client-to-server stream.
// It reports at most one target and is not safe for concurrent use.
type ShadowsocksTCPSniffer struct {
	spec   shadowsocksCipherSpec
	key    []byte // Legacy master key or 2022 user key, used to derive session keys.
	eihLen int
	limit  int
	buffer []byte
	done   bool
}

// NewShadowsocksTCPSniffer returns a sniffer for the matching landing server,
// or nil when no usable entry matches. Observe is safe to call on a nil sniffer.
func NewShadowsocksTCPSniffer(configs []ShadowsocksConfig, host string, port int) *ShadowsocksTCPSniffer {
	config, ok := matchingShadowsocksConfig(configs, host, port)
	if !ok {
		return nil
	}
	spec, ok := shadowsocksCipherSpecForMethod(config.Method)
	if !ok {
		return nil
	}
	sniffer := &ShadowsocksTCPSniffer{spec: spec}
	if spec.is2022 {
		keys, ok := parseShadowsocks2022TCPKeys(config.Password, spec.keySize)
		if !ok {
			return nil
		}
		sniffer.key = keys.userKey
		sniffer.eihLen = keys.eihLen
	} else {
		sniffer.key = evpBytesToKey([]byte(config.Password), spec.keySize)
	}
	sniffer.limit = spec.tcpBufferLimit(sniffer.eihLen)
	return sniffer
}

// Observe consumes the next stream bytes and reports the inner target once. A
// false result does not distinguish incomplete input from terminal mismatch.
func (s *ShadowsocksTCPSniffer) Observe(p []byte) (string, bool) {
	if s == nil || s.done || len(p) == 0 {
		return "", false
	}
	limit := s.limit
	if limit <= 0 {
		s.done = true
		return "", false
	}
	if len(s.buffer) < limit {
		n := min(len(p), limit-len(s.buffer))
		s.buffer = append(s.buffer, p[:n]...)
	}
	target, state := s.sniff(s.buffer)
	switch state {
	case sniffFound:
		s.done = true
		return target, true
	case sniffFailed:
		s.done = true
	case sniffNeedMore:
		if len(s.buffer) >= limit {
			s.done = true
		}
	}
	return "", false
}

func (spec shadowsocksCipherSpec) tcpBufferLimit(eihLen int) int {
	if spec.is2022 {
		return spec.saltSize + eihLen +
			shadowsocks2022FixedHeaderLen + shadowsocksAEADTagSize +
			shadowsocks2022MaxVariableHeader + shadowsocksAEADTagSize
	}
	return spec.saltSize + shadowsocksAEADLengthFieldSize + shadowsocksAEADTagSize +
		shadowsocksMaxChunkPayload + shadowsocksAEADTagSize
}

func (s *ShadowsocksTCPSniffer) sniff(data []byte) (string, shadowsocksSniffState) {
	if s.spec.is2022 {
		return s.sniff2022(data)
	}
	return s.sniffAEAD(data)
}

func (s *ShadowsocksTCPSniffer) sniffAEAD(data []byte) (string, shadowsocksSniffState) {
	if len(data) < s.spec.saltSize+shadowsocksAEADLengthFieldSize+shadowsocksAEADTagSize {
		return "", sniffNeedMore
	}

	salt := data[:s.spec.saltSize]
	aead, err := s.spec.sessionAEAD(s.key, salt)
	if err != nil {
		return "", sniffFailed
	}

	var nonce [shadowsocksAEADNonceSize]byte
	lengthStart := s.spec.saltSize
	lengthEnd := lengthStart + shadowsocksAEADLengthFieldSize + shadowsocksAEADTagSize
	lengthPlaintext, err := aead.Open(nil, nonce[:], data[lengthStart:lengthEnd], nil)
	if err != nil {
		return "", sniffFailed
	}
	if len(lengthPlaintext) != shadowsocksAEADLengthFieldSize {
		return "", sniffFailed
	}
	payloadLen := int(binary.BigEndian.Uint16(lengthPlaintext))
	if payloadLen <= 0 || payloadLen > shadowsocksMaxChunkPayload {
		return "", sniffFailed
	}

	payloadStart := lengthEnd
	payloadEnd := payloadStart + payloadLen + shadowsocksAEADTagSize
	if len(data) < payloadEnd {
		return "", sniffNeedMore
	}
	incrementShadowsocksNonce(nonce[:])
	payload, err := aead.Open(nil, nonce[:], data[payloadStart:payloadEnd], nil)
	if err != nil {
		return "", sniffFailed
	}
	target, ok := parseShadowsocksTargetAddress(payload)
	if !ok {
		return "", sniffFailed
	}
	return target, sniffFound
}

func (s *ShadowsocksTCPSniffer) sniff2022(data []byte) (string, shadowsocksSniffState) {
	fixedStart := s.spec.saltSize + s.eihLen
	fixedEnd := fixedStart + shadowsocks2022FixedHeaderLen + shadowsocksAEADTagSize
	if len(data) < fixedEnd {
		return "", sniffNeedMore
	}

	salt := data[:s.spec.saltSize]
	aead, err := s.spec.sessionAEAD(s.key, salt)
	if err != nil {
		return "", sniffFailed
	}

	var nonce [shadowsocksAEADNonceSize]byte
	fixedHeader, err := aead.Open(nil, nonce[:], data[fixedStart:fixedEnd], nil)
	if err != nil {
		return "", sniffFailed
	}
	if len(fixedHeader) != shadowsocks2022FixedHeaderLen || fixedHeader[0] != 0 {
		return "", sniffFailed
	}
	variableLen := int(binary.BigEndian.Uint16(
		fixedHeader[shadowsocks2022HeaderLenOffset:shadowsocks2022FixedHeaderLen],
	))
	if variableLen <= 0 {
		return "", sniffFailed
	}

	variableStart := fixedEnd
	variableEnd := variableStart + variableLen + shadowsocksAEADTagSize
	if len(data) < variableEnd {
		return "", sniffNeedMore
	}
	incrementShadowsocksNonce(nonce[:])
	variableHeader, err := aead.Open(nil, nonce[:], data[variableStart:variableEnd], nil)
	if err != nil {
		return "", sniffFailed
	}
	target, ok := parseShadowsocksTargetAddress(variableHeader)
	if !ok {
		return "", sniffFailed
	}
	return target, sniffFound
}
