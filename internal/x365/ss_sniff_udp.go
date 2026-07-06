package x365

import (
	"crypto/aes"
	"encoding/binary"
	"time"
)

const (
	shadowsocks2022UDPTypeOffset       = 0
	shadowsocks2022UDPEpochOffset      = 1
	shadowsocks2022UDPPaddingLenOffset = 9
	shadowsocks2022UDPHeaderLen        = 11
)

func sniffShadowsocksUDPTarget(config ShadowsocksSniffConfig, data []byte) (string, bool) {
	spec, ok := ssCipherSpecForMethod(config.Method)
	if !ok {
		return "", false
	}
	if spec.is2022 {
		return sniffShadowsocks2022UDPTarget(config, spec, data)
	}
	masterKey := evpBytesToKey([]byte(config.Password), spec.keySize)
	return sniffShadowsocksAEADUDPTarget(spec, masterKey, data)
}

func SniffShadowsocksUDPChain(configs []ShadowsocksSniffConfig, host string, port int, payload []byte) (string, bool) {
	config, ok := matchingShadowsocksSniffConfig(configs, host, port)
	if !ok {
		return "", false
	}
	return sniffShadowsocksUDPTarget(config, payload)
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

func sniffShadowsocks2022UDPTarget(config ShadowsocksSniffConfig, spec ssCipherSpec, data []byte) (string, bool) {
	candidate, ok := shadowsocks2022UDPKeyCandidateFor(config.Password, spec.keySize)
	if !ok {
		return "", false
	}
	return sniffShadowsocks2022UDPTargetWithKey(candidate, spec, data)
}

func sniffShadowsocks2022UDPTargetWithKey(
	candidate shadowsocks2022KeyCandidate,
	spec ssCipherSpec,
	data []byte,
) (string, bool) {
	plain, ok := decryptShadowsocks2022UDPPayload(candidate, spec, data)
	if !ok {
		return "", false
	}
	return parseShadowsocks2022UDPPlaintext(plain, time.Now())
}

func decryptShadowsocks2022UDPPayload(
	candidate shadowsocks2022KeyCandidate,
	spec ssCipherSpec,
	data []byte,
) ([]byte, bool) {
	ciphertextStart := aes.BlockSize + candidate.eihLen
	if len(data) < ciphertextStart+shadowsocks2022UDPHeaderLen+shadowsocksAEADTagSize {
		return nil, false
	}
	block, err := aes.NewCipher(candidate.blockKey)
	if err != nil {
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
