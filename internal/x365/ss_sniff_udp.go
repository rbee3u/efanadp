package x365

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

// ShadowsocksUDPSniffer reads the inner destination out of Shadowsocks UDP
// datagrams bound for one configured server.
//
// It exists so the per-config key material is derived once instead of once per
// datagram. Every datagram on a UDP association is a candidate, and the
// derivations are not cheap: the pre-2022 methods stretch the password through
// chained MD5 rounds, and the 2022 methods base64-decode the PSK and expand an
// AES key schedule. Only the salt-dependent steps genuinely have to run per
// datagram, so deriving the rest per datagram spends real CPU on a relay hot
// path for a result that never changes.
type ShadowsocksUDPSniffer struct {
	spec      ssCipherSpec
	masterKey []byte
	candidate shadowsocks2022KeyCandidate
	block     cipher.Block
}

// NewShadowsocksUDPSniffer returns a sniffer for the config matching host and
// port, or nil when no config matches or its method is not supported. A nil
// sniffer is usable: Sniff reports no target.
func NewShadowsocksUDPSniffer(configs []ShadowsocksSniffConfig, host string, port int) *ShadowsocksUDPSniffer {
	config, ok := matchingShadowsocksSniffConfig(configs, host, port)
	if !ok {
		return nil
	}
	spec, ok := ssCipherSpecForMethod(config.Method)
	if !ok {
		return nil
	}
	if !spec.is2022 {
		return &ShadowsocksUDPSniffer{
			spec:      spec,
			masterKey: evpBytesToKey([]byte(config.Password), spec.keySize),
		}
	}
	candidate, ok := shadowsocks2022UDPKeyCandidateFor(config.Password, spec.keySize)
	if !ok {
		return nil
	}
	block, err := aes.NewCipher(candidate.blockKey)
	if err != nil {
		return nil
	}
	return &ShadowsocksUDPSniffer{spec: spec, candidate: candidate, block: block}
}

// Sniff reports the inner destination carried by one datagram. It is not safe
// for concurrent use: the 2022 path decrypts through a shared cipher.Block.
func (s *ShadowsocksUDPSniffer) Sniff(payload []byte) (string, bool) {
	if s == nil {
		return "", false
	}
	if s.spec.is2022 {
		return sniffShadowsocks2022UDPTargetWithBlock(s.candidate, s.spec, s.block, payload)
	}
	return sniffShadowsocksAEADUDPTarget(s.spec, s.masterKey, payload)
}

func sniffShadowsocksAEADUDPTarget(spec ssCipherSpec, masterKey []byte, data []byte) (string, bool) {
	if len(data) < spec.saltSize+shadowsocksAEADTagSize+1 {
		return "", false
	}
	salt := data[:spec.saltSize]
	subkey, err := deriveShadowsocksSubkeyFromMasterKey(masterKey, salt, spec.keySize)
	if err != nil {
		return "", false
	}
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		return "", false
	}
	nonce := make([]byte, aead.NonceSize())
	payload, err := aead.Open(nil, nonce, data[spec.saltSize:], nil)
	if err != nil {
		return "", false
	}
	target, ok := parseShadowsocksTargetAddress(payload)
	return target, ok
}

func sniffShadowsocks2022UDPTargetWithBlock(
	candidate shadowsocks2022KeyCandidate,
	spec ssCipherSpec,
	block cipher.Block,
	data []byte,
) (string, bool) {
	plain, ok := decryptShadowsocks2022UDPPayload(candidate, spec, block, data)
	if !ok {
		return "", false
	}
	return parseShadowsocks2022UDPPlaintext(plain, time.Now())
}

func decryptShadowsocks2022UDPPayload(
	candidate shadowsocks2022KeyCandidate,
	spec ssCipherSpec,
	block cipher.Block,
	data []byte,
) ([]byte, bool) {
	ciphertextStart := aes.BlockSize + candidate.eihLen
	if len(data) < ciphertextStart+shadowsocks2022UDPHeaderLen+shadowsocksAEADTagSize {
		return nil, false
	}
	if block == nil {
		return nil, false
	}
	var packetHeader [aes.BlockSize]byte
	copy(packetHeader[:], data[:aes.BlockSize])
	block.Decrypt(packetHeader[:], packetHeader[:])

	sessionKey := deriveShadowsocks2022Subkey(candidate.userKey, packetHeader[:8], spec.keySize)
	aead, err := spec.newAEAD(sessionKey)
	if err != nil {
		return nil, false
	}
	nonce := packetHeader[4:16]
	plain, err := aead.Open(nil, nonce, data[ciphertextStart:], nil)
	if err != nil {
		return nil, false
	}
	return plain, true
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
	target, ok := parseShadowsocksTargetAddress(plain[offset:])
	return target, ok
}

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
