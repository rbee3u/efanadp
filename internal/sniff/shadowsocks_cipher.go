package sniff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"strings"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	shadowsocksAEADTagSize   = 16
	shadowsocksAEADNonceSize = 12
	shadowsocks2022Context   = "shadowsocks 2022 session subkey"
)

type shadowsocksCipherSpec struct {
	keySize  int
	saltSize int
	newAEAD  func([]byte) (cipher.AEAD, error)
	is2022   bool
}

func shadowsocksCipherSpecForMethod(method string) (shadowsocksCipherSpec, bool) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "aes-128-gcm":
		return shadowsocksCipherSpec{keySize: 16, saltSize: 16, newAEAD: newAESGCM}, true
	case "aes-256-gcm":
		return shadowsocksCipherSpec{keySize: 32, saltSize: 32, newAEAD: newAESGCM}, true
	case "chacha20-ietf-poly1305":
		return shadowsocksCipherSpec{keySize: 32, saltSize: 32, newAEAD: chacha20poly1305.New}, true
	case "2022-blake3-aes-128-gcm":
		return shadowsocksCipherSpec{keySize: 16, saltSize: 16, newAEAD: newAESGCM, is2022: true}, true
	case "2022-blake3-aes-256-gcm":
		return shadowsocksCipherSpec{keySize: 32, saltSize: 32, newAEAD: newAESGCM, is2022: true}, true
	default:
		return shadowsocksCipherSpec{}, false
	}
}

type shadowsocks2022Keys struct {
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

// parseShadowsocks2022TCPKeys reads the final PSK as the user key. Each leading
// identity PSK contributes a 16-byte EIH block to skip before the payload.
func parseShadowsocks2022TCPKeys(password string, keySize int) (shadowsocks2022Keys, bool) {
	parts := strings.Split(password, ":")
	userKey, ok := decodeShadowsocks2022PSK(parts[len(parts)-1], keySize)
	if !ok {
		return shadowsocks2022Keys{}, false
	}
	return shadowsocks2022Keys{
		userKey: userKey,
		eihLen:  16 * (len(parts) - 1),
	}, true
}

func parseShadowsocks2022UDPKeys(password string, keySize int) (shadowsocks2022Keys, bool) {
	keys, ok := parseShadowsocks2022TCPKeys(password, keySize)
	if !ok {
		return shadowsocks2022Keys{}, false
	}
	keys.blockKey = keys.userKey
	if identityKey, _, hasIdentity := strings.Cut(password, ":"); hasIdentity {
		keys.blockKey, ok = decodeShadowsocks2022PSK(identityKey, keySize)
		if !ok {
			return shadowsocks2022Keys{}, false
		}
	}
	return keys, true
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

func (spec shadowsocksCipherSpec) sessionAEAD(key, salt []byte) (cipher.AEAD, error) {
	if spec.is2022 {
		return spec.newAEAD(deriveShadowsocks2022Subkey(key, salt, spec.keySize))
	}
	subkey, err := deriveShadowsocksSubkeyFromMasterKey(key, salt, spec.keySize)
	if err != nil {
		return nil, err
	}
	return spec.newAEAD(subkey)
}

func deriveShadowsocksSubkeyFromMasterKey(masterKey []byte, salt []byte, keySize int) ([]byte, error) {
	return hkdf.Key(sha1.New, masterKey, salt, "ss-subkey", keySize)
}

// The pre-2022 protocol requires OpenSSL's chained-MD5 KDF.
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

func newAESGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
