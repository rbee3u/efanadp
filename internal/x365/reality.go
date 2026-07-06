package x365

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
)

type realityClientConfig struct {
	Fingerprint utls.ClientHelloID
	ServerName  string
	PublicKey   []byte
	ShortID     []byte
}

const (
	realityPublicKeySize = 32
	realityShortIDMaxLen = 8
)

func buildRealityConfig(node Node) (*realityClientConfig, error) {
	publicKey, err := parseRealityPublicKey(node.PublicKey)
	if err != nil {
		return nil, err
	}
	shortID, err := parseRealityShortID(node.ShortID)
	if err != nil {
		return nil, err
	}
	serverName := node.SNI
	if serverName == "" {
		serverName = node.Host
	}
	if serverName == "" {
		return nil, errors.New("reality server name is empty")
	}
	return &realityClientConfig{
		Fingerprint: realityFingerprint(node.realityFingerprint),
		ServerName:  serverName,
		PublicKey:   publicKey,
		ShortID:     shortID,
	}, nil
}

func parseRealityPublicKey(value string) ([]byte, error) {
	publicKey, err := decodeBase64URL(value)
	if err != nil {
		return nil, fmt.Errorf("invalid reality public key: %w", err)
	}
	if len(publicKey) != realityPublicKeySize {
		return nil, fmt.Errorf("reality public key must be %d bytes, got %d", realityPublicKeySize, len(publicKey))
	}
	return publicKey, nil
}

func parseRealityShortID(value string) ([]byte, error) {
	shortID, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid reality short id: %w", err)
	}
	if len(shortID) > realityShortIDMaxLen {
		return nil, fmt.Errorf("reality short id must be at most %d bytes, got %d", realityShortIDMaxLen, len(shortID))
	}
	return shortID, nil
}

func decodeBase64URL(value string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func realityFingerprint(value string) utls.ClientHelloID {
	switch value {
	case "firefox":
		return utls.HelloFirefox_Auto
	case "safari":
		return utls.HelloSafari_Auto
	case "ios":
		return utls.HelloIOS_Auto
	case "edge":
		return utls.HelloEdge_Auto
	case "random", "randomized":
		return utls.HelloRandomizedALPN
	default:
		return utls.HelloChrome_133
	}
}

func dialReality(ctx context.Context, raw net.Conn, config *realityClientConfig) (net.Conn, error) {
	var (
		authKey  []byte
		verified bool
	)
	verifyPeer := func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		certs := make([]*x509.Certificate, 0, len(rawCerts))
		for _, rawCert := range rawCerts {
			cert, err := x509.ParseCertificate(rawCert)
			if err != nil {
				return err
			}
			certs = append(certs, cert)
		}
		if len(certs) == 0 {
			return errors.New("reality peer sent no certificate")
		}
		if pub, ok := certs[0].PublicKey.(ed25519.PublicKey); ok && len(authKey) != 0 {
			h := hmac.New(sha512.New, authKey)
			_, _ = h.Write(pub)
			if hmac.Equal(h.Sum(nil), certs[0].Signature) {
				verified = true
				return nil
			}
		}
		intermediates := x509.NewCertPool()
		for _, cert := range certs[1:] {
			intermediates.AddCert(cert)
		}
		_, err := certs[0].Verify(x509.VerifyOptions{
			DNSName:       config.ServerName,
			Intermediates: intermediates,
		})
		return err
	}

	uConn := utls.UClient(raw, &utls.Config{
		ServerName:             config.ServerName,
		InsecureSkipVerify:     true,
		SessionTicketsDisabled: true,
		VerifyPeerCertificate:  verifyPeer,
	}, config.Fingerprint)
	if err := uConn.BuildHandshakeState(); err != nil {
		return nil, err
	}
	hello := uConn.HandshakeState.Hello
	if len(hello.Raw) < 71 || len(hello.Random) < 32 {
		return nil, fmt.Errorf("client hello is too short for REALITY patch: raw=%d random=%d", len(hello.Raw), len(hello.Random))
	}
	hello.SessionId = make([]byte, 32)
	copy(hello.Raw[39:], hello.SessionId)
	hello.SessionId[0] = 26
	hello.SessionId[1] = 2
	hello.SessionId[2] = 6
	hello.SessionId[3] = 0
	now := time.Now().Unix()
	if now < 0 || now > 1<<32-1 {
		return nil, fmt.Errorf("REALITY unix time out of uint32 range: %d", now)
	}
	binary.BigEndian.PutUint32(hello.SessionId[4:], uint32(now))
	copy(hello.SessionId[8:], config.ShortID)

	publicKey, err := ecdh.X25519().NewPublicKey(config.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("bad reality public key: %w", err)
	}
	keyShares := uConn.HandshakeState.State13.KeyShareKeys
	if keyShares == nil {
		return nil, errors.New("realityFingerprint did not build a TLS 1.3 key share")
	}
	ecdhe := keyShares.Ecdhe
	if ecdhe == nil {
		ecdhe = keyShares.MlkemEcdhe
	}
	if ecdhe == nil {
		return nil, errors.New("realityFingerprint does not support REALITY X25519 key share")
	}
	authKey, err = ecdhe.ECDH(publicKey)
	if err != nil {
		return nil, fmt.Errorf("derive reality shared key: %w", err)
	}
	authKey, err = hkdf.Key(sha256.New, authKey, hello.Random[:20], "REALITY", len(authKey))
	if err != nil {
		return nil, err
	}
	aead, err := newAESGCM(authKey)
	if err != nil {
		return nil, err
	}
	aead.Seal(hello.SessionId[:0], hello.Random[20:], hello.SessionId[:16], hello.Raw)
	copy(hello.Raw[39:], hello.SessionId)

	if err := uConn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	if !verified {
		closeQuietly(uConn)
		return nil, errors.New("reality peer was not authenticated")
	}
	return uConn, nil
}
