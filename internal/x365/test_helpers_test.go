package x365

import (
	"crypto/aes"
	"errors"
	"io"
	"testing"
	"time"
)

const testWaitTimeout = time.Second

func parseFrame(buf []byte) (frame, int, error) {
	if len(buf) < frameHeaderSize {
		return frame{}, 0, errors.New("need more data")
	}
	header, payloadLen := parseFrameHeader(buf[:frameHeaderSize])
	total := frameHeaderSize + payloadLen
	if len(buf) < total {
		return frame{}, 0, errors.New("need more data")
	}
	header.Payload = append([]byte(nil), buf[frameHeaderSize:total]...)
	return header, total, nil
}

func readFrameForTest(r io.Reader) (frame, error) {
	result, payloadLen, err := readFrameHeader(r)
	if err != nil {
		return frame{}, err
	}
	if payloadLen > 0 {
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return frame{}, err
		}
		result.Payload = payload
	}
	return result, nil
}

func shadowsocksSniffBufferLimit(method string) int {
	return shadowsocksSniffBufferLimitForConfig(ShadowsocksSniffConfig{Method: method})
}

func shadowsocksSniffBufferLimitForConfig(config ShadowsocksSniffConfig) int {
	method := config.Method
	spec, ok := ssCipherSpecForMethod(method)
	if !ok {
		return 0
	}
	return shadowsocksSniffBufferLimitForSpec(config, spec)
}

func sniffShadowsocksTarget(config ShadowsocksSniffConfig, data []byte) (string, shadowsocksSniffState) {
	spec, ok := ssCipherSpecForMethod(config.Method)
	if !ok {
		return "", sniffFailed
	}
	if spec.is2022 {
		return sniffShadowsocks2022Target(config, spec, data)
	}
	masterKey := evpBytesToKey([]byte(config.Password), spec.keySize)
	return sniffShadowsocksTargetWithMasterKey(spec, masterKey, data)
}

func sniffShadowsocks2022Target(config ShadowsocksSniffConfig, spec ssCipherSpec, data []byte) (string, shadowsocksSniffState) {
	candidate, ok := shadowsocks2022KeyCandidateFor(config.Password, spec.keySize)
	if !ok {
		return "", sniffFailed
	}
	return sniffShadowsocks2022TargetWithKey(candidate, spec, data)
}

// sniffShadowsocksUDPTarget derives the key material for config and sniffs one
// datagram with it. Production sniffing goes through ShadowsocksUDPSniffer,
// which derives that material once per destination instead of once per datagram;
// this keeps the per-datagram form available to tests that check a single packet.
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
	block, err := aes.NewCipher(candidate.blockKey)
	if err != nil {
		return "", false
	}
	return sniffShadowsocks2022UDPTargetWithBlock(candidate, spec, block, data)
}

func deriveShadowsocksSubkey(password string, salt []byte, keySize int) ([]byte, error) {
	masterKey := evpBytesToKey([]byte(password), keySize)
	return deriveShadowsocksSubkeyFromMasterKey(masterKey, salt, keySize)
}

func receiveWithin[T any](tb testing.TB, ch <-chan T, label string) T {
	tb.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(testWaitTimeout):
		tb.Fatalf("timed out waiting for %s", label)
	}
	var zero T
	return zero
}

func requireNoReceive[T any](tb testing.TB, ch <-chan T, label string) {
	tb.Helper()
	select {
	case value := <-ch:
		tb.Fatalf("received %s: %v", label, value)
	default:
	}
}
