package x365

import (
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"strings"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/chacha20poly1305"
)

type ssCipherSpec struct {
	keySize  int
	saltSize int
	newAEAD  func([]byte) (cipher.AEAD, error)
	is2022   bool
}

func ssCipherSpecForMethod(method string) (ssCipherSpec, bool) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "aes-128-gcm":
		return ssCipherSpec{keySize: 16, saltSize: 16, newAEAD: newAESGCM}, true
	case "aes-256-gcm":
		return ssCipherSpec{keySize: 32, saltSize: 32, newAEAD: newAESGCM}, true
	case "chacha20-ietf-poly1305":
		return ssCipherSpec{keySize: 32, saltSize: 32, newAEAD: chacha20poly1305.New}, true
	case "2022-blake3-aes-128-gcm":
		return ssCipherSpec{keySize: 16, saltSize: 16, newAEAD: newAESGCM, is2022: true}, true
	case "2022-blake3-aes-256-gcm":
		return ssCipherSpec{keySize: 32, saltSize: 32, newAEAD: newAESGCM, is2022: true}, true
	default:
		return ssCipherSpec{}, false
	}
}

type shadowsocks2022KeyCandidate struct {
	blockKey []byte
	userKey  []byte
	eihLen   int
}

func deriveShadowsocks2022Subkey(key []byte, salt []byte, keySize int) []byte {
	keyMaterial := make([]byte, 0, len(key)+len(salt))
	keyMaterial = append(keyMaterial, key...)
	keyMaterial = append(keyMaterial, salt...)
	subkey := make([]byte, 32)
	blake3.DeriveKey(shadowsocks2022Context, keyMaterial, subkey)
	return subkey[:keySize]
}

func shadowsocks2022KeyCandidateFor(password string, keySize int) (shadowsocks2022KeyCandidate, bool) {
	userKey, eihLen, ok := shadowsocks2022UserKey(password, keySize)
	if !ok {
		return shadowsocks2022KeyCandidate{}, false
	}
	return shadowsocks2022KeyCandidate{
		userKey: userKey,
		eihLen:  eihLen,
	}, true
}

func shadowsocks2022UserKey(password string, keySize int) ([]byte, int, bool) {
	parts := strings.Split(password, ":")
	rawKey := password
	eihLen := 0
	if len(parts) > 1 {
		rawKey = parts[len(parts)-1]
		eihLen = 16 * (len(parts) - 1)
	}
	key, ok := decodeShadowsocks2022PSK(rawKey, keySize)
	return key, eihLen, ok
}

func shadowsocks2022UDPKeyCandidateFor(password string, keySize int) (shadowsocks2022KeyCandidate, bool) {
	userKey, eihLen, ok := shadowsocks2022UserKey(password, keySize)
	if !ok {
		return shadowsocks2022KeyCandidate{}, false
	}
	parts := strings.Split(password, ":")
	if len(parts) > 1 {
		blockKey, ok := decodeShadowsocks2022PSK(parts[0], keySize)
		if !ok {
			return shadowsocks2022KeyCandidate{}, false
		}
		return shadowsocks2022KeyCandidate{
			blockKey: blockKey,
			userKey:  userKey,
			eihLen:   eihLen,
		}, true
	}
	return shadowsocks2022KeyCandidate{blockKey: userKey, userKey: userKey}, true
}

func shadowsocks2022EIHLen(password string) int {
	if strings.Count(password, ":") == 0 {
		return 0
	}
	return 16 * strings.Count(password, ":")
}

func decodeShadowsocks2022PSK(password string, keySize int) ([]byte, bool) {
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		key, err := encoding.DecodeString(password)
		if err == nil && len(key) == keySize {
			return key, true
		}
	}
	if len(password) == keySize {
		return []byte(password), true
	}
	return nil, false
}

func deriveShadowsocksSubkeyFromMasterKey(masterKey []byte, salt []byte, keySize int) ([]byte, error) {
	return hkdf.Key(sha1.New, masterKey, salt, "ss-subkey", keySize)
}

func evpBytesToKey(password []byte, keySize int) []byte {
	var key []byte
	var previous []byte
	for len(key) < keySize {
		digest := md5.New()
		_, _ = digest.Write(previous)
		_, _ = digest.Write(password)
		previous = digest.Sum(nil)
		key = append(key, previous...)
	}
	return key[:keySize]
}

func incrementShadowsocksNonce(nonce []byte) {
	for i := range nonce {
		nonce[i]++
		if nonce[i] != 0 {
			return
		}
	}
}
