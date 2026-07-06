package x365

import (
	"encoding/binary"
)

const (
	shadowsocksAEADTagSize     = 16
	shadowsocksAEADNonceSize   = 12
	shadowsocksMaxChunkPayload = 0x3fff
	shadowsocks2022FixedHeader = 11
	shadowsocks2022Context     = "shadowsocks 2022 session subkey"
)

type shadowsocksSniffState int

const (
	sniffNeedMore shadowsocksSniffState = iota
	sniffFound
	sniffFailed
)

type ShadowsocksTargetSniffer struct {
	spec      ssCipherSpec
	limit     int
	masterKey []byte
	candidate shadowsocks2022KeyCandidate
	buffer    []byte
	done      bool
}

func NewShadowsocksTargetSniffer(configs []ShadowsocksSniffConfig, host string, port int) *ShadowsocksTargetSniffer {
	config, ok := matchingShadowsocksSniffConfig(configs, host, port)
	if !ok {
		return nil
	}
	spec, ok := ssCipherSpecForMethod(config.Method)
	if !ok {
		return nil
	}
	sniffer := &ShadowsocksTargetSniffer{
		spec:  spec,
		limit: shadowsocksSniffBufferLimitForSpec(config, spec),
	}
	if sniffer.limit <= 0 {
		return nil
	}
	if spec.is2022 {
		candidate, ok := shadowsocks2022KeyCandidateFor(config.Password, spec.keySize)
		if !ok {
			return nil
		}
		sniffer.candidate = candidate
	} else {
		sniffer.masterKey = evpBytesToKey([]byte(config.Password), spec.keySize)
	}
	return sniffer
}

func matchingShadowsocksSniffConfig(configs []ShadowsocksSniffConfig, host string, port int) (ShadowsocksSniffConfig, bool) {
	for _, config := range configs {
		if config.Port != port {
			continue
		}
		if sameHost(config.Server, host) {
			return config, true
		}
	}
	return ShadowsocksSniffConfig{}, false
}

func (s *ShadowsocksTargetSniffer) Observe(p []byte) (string, bool) {
	if s == nil || s.done || len(p) == 0 {
		return "", false
	}
	limit := s.limit
	if limit <= 0 {
		s.done = true
		return "", false
	}
	if len(s.buffer) < limit {
		remaining := limit - len(s.buffer)
		if len(p) > remaining {
			p = p[:remaining]
		}
		s.buffer = append(s.buffer, p...)
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

func shadowsocksSniffBufferLimitForSpec(config ShadowsocksSniffConfig, spec ssCipherSpec) int {
	if spec.is2022 {
		return spec.saltSize + shadowsocks2022EIHLen(config.Password) + shadowsocks2022FixedHeader + shadowsocksAEADTagSize + 0xffff + shadowsocksAEADTagSize
	}
	return spec.saltSize + 2 + shadowsocksAEADTagSize + shadowsocksMaxChunkPayload + shadowsocksAEADTagSize
}

func (s *ShadowsocksTargetSniffer) sniff(data []byte) (string, shadowsocksSniffState) {
	if s.spec.is2022 {
		return sniffShadowsocks2022TargetWithKey(s.candidate, s.spec, data)
	}
	return sniffShadowsocksTargetWithMasterKey(s.spec, s.masterKey, data)
}

func sniffShadowsocksTargetWithMasterKey(spec ssCipherSpec, masterKey []byte, data []byte) (string, shadowsocksSniffState) {
	if len(data) < spec.saltSize+2+shadowsocksAEADTagSize {
		return "", sniffNeedMore
	}

	salt := data[:spec.saltSize]
	subkey, err := deriveShadowsocksSubkeyFromMasterKey(masterKey, salt, spec.keySize)
	if err != nil {
		return "", sniffFailed
	}
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		return "", sniffFailed
	}

	var nonce [shadowsocksAEADNonceSize]byte
	lengthStart := spec.saltSize
	lengthEnd := lengthStart + 2 + shadowsocksAEADTagSize
	lengthPlaintext, err := aead.Open(nil, nonce[:], data[lengthStart:lengthEnd], nil)
	if err != nil {
		return "", sniffFailed
	}
	if len(lengthPlaintext) != 2 {
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

func sniffShadowsocks2022TargetWithKey(candidate shadowsocks2022KeyCandidate, spec ssCipherSpec, data []byte) (string, shadowsocksSniffState) {
	fixedStart := spec.saltSize + candidate.eihLen
	fixedEnd := fixedStart + shadowsocks2022FixedHeader + shadowsocksAEADTagSize
	if len(data) < fixedEnd {
		return "", sniffNeedMore
	}

	salt := data[:spec.saltSize]
	subkey := deriveShadowsocks2022Subkey(candidate.userKey, salt, spec.keySize)
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		return "", sniffFailed
	}

	var nonce [shadowsocksAEADNonceSize]byte
	fixedHeader, err := aead.Open(nil, nonce[:], data[fixedStart:fixedEnd], nil)
	if err != nil {
		return "", sniffFailed
	}
	if len(fixedHeader) != shadowsocks2022FixedHeader || fixedHeader[0] != 0 {
		return "", sniffFailed
	}
	variableLen := int(binary.BigEndian.Uint16(fixedHeader[9:11]))
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
