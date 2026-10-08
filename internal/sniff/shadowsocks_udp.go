package sniff

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"time"
)

const (
	shadowsocks2022UDPTypeOffset       = 0
	shadowsocks2022UDPEpochOffset      = 1
	shadowsocks2022UDPPaddingLenOffset = 9
	shadowsocks2022UDPHeaderLen        = 11
)

// ShadowsocksUDPSniffer inspects UDP targets, caching server key material
// across datagrams.
type ShadowsocksUDPSniffer struct {
	spec   shadowsocksCipherSpec
	key    []byte // Legacy master key or 2022 user key, used to derive session keys.
	eihLen int
	block  cipher.Block
}

// NewShadowsocksUDPSniffer returns a sniffer for the config matching host and
// port, or nil when no config matches or its method is not supported. A nil
// sniffer is usable: Sniff reports no target.
func NewShadowsocksUDPSniffer(configs []ShadowsocksConfig, host string, port int) *ShadowsocksUDPSniffer {
	config, ok := matchingShadowsocksConfig(configs, host, port)
	if !ok {
		return nil
	}
	spec, ok := shadowsocksCipherSpecForMethod(config.Method)
	if !ok {
		return nil
	}
	sniffer := &ShadowsocksUDPSniffer{spec: spec}
	if spec.is2022 {
		keys, ok := parseShadowsocks2022UDPKeys(config.Password, spec.keySize)
		if !ok {
			return nil
		}
		block, err := aes.NewCipher(keys.blockKey)
		if err != nil {
			return nil
		}
		sniffer.key = keys.userKey
		sniffer.eihLen = keys.eihLen
		sniffer.block = block
	} else {
		sniffer.key = evpBytesToKey([]byte(config.Password), spec.keySize)
	}
	return sniffer
}

// Sniff reports the inner destination carried by one datagram. It is not safe
// for concurrent use: the 2022 path decrypts through a shared cipher.Block.
func (s *ShadowsocksUDPSniffer) Sniff(payload []byte) (string, bool) {
	if s == nil {
		return "", false
	}
	if s.spec.is2022 {
		return s.sniff2022(payload)
	}
	return s.sniffAEAD(payload)
}

func (s *ShadowsocksUDPSniffer) sniffAEAD(data []byte) (string, bool) {
	if len(data) < s.spec.saltSize+shadowsocksAEADTagSize+1 {
		return "", false
	}
	salt := data[:s.spec.saltSize]
	aead, err := s.spec.sessionAEAD(s.key, salt)
	if err != nil {
		return "", false
	}
	nonce := make([]byte, aead.NonceSize())
	payload, err := aead.Open(nil, nonce, data[s.spec.saltSize:], nil)
	if err != nil {
		return "", false
	}
	return parseShadowsocksTargetAddress(payload)
}

func (s *ShadowsocksUDPSniffer) sniff2022(data []byte) (string, bool) {
	ciphertextStart := aes.BlockSize + s.eihLen
	if len(data) < ciphertextStart+shadowsocks2022UDPHeaderLen+shadowsocksAEADTagSize {
		return "", false
	}
	if s.block == nil {
		return "", false
	}
	var packetHeader [aes.BlockSize]byte
	copy(packetHeader[:], data[:aes.BlockSize])
	s.block.Decrypt(packetHeader[:], packetHeader[:])

	aead, err := s.spec.sessionAEAD(s.key, packetHeader[:8])
	if err != nil {
		return "", false
	}
	nonce := packetHeader[4:16]
	plain, err := aead.Open(nil, nonce, data[ciphertextStart:], nil)
	if err != nil {
		return "", false
	}
	return parseShadowsocks2022UDPPlaintext(plain, time.Now())
}

func parseShadowsocks2022UDPPlaintext(plain []byte, now time.Time) (string, bool) {
	if len(plain) < shadowsocks2022UDPHeaderLen {
		return "", false
	}
	if plain[shadowsocks2022UDPTypeOffset] != 0 {
		return "", false
	}
	epochValue := binary.BigEndian.Uint64(plain[shadowsocks2022UDPEpochOffset:shadowsocks2022UDPPaddingLenOffset])
	if !freshShadowsocks2022UDPEpoch(epochValue, now) {
		return "", false
	}
	paddingLen := int(binary.BigEndian.Uint16(plain[shadowsocks2022UDPPaddingLenOffset:shadowsocks2022UDPHeaderLen]))
	offset := shadowsocks2022UDPHeaderLen + paddingLen
	if len(plain) <= offset {
		return "", false
	}
	return parseShadowsocksTargetAddress(plain[offset:])
}

// Use the two-minute skew window as a plausibility check, not replay
// protection. Accept zero timestamps for clients that leave the field unset.
func freshShadowsocks2022UDPEpoch(epochValue uint64, now time.Time) bool {
	const maxInt64AsUint64 = uint64(1<<63 - 1)

	if epochValue > maxInt64AsUint64 {
		return false
	}
	epoch := int64(epochValue)
	if epoch == 0 {
		return true
	}
	diff := now.Sub(time.Unix(epoch, 0))
	return diff >= -2*time.Minute && diff <= 2*time.Minute
}
