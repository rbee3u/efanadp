package sniff

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestSniffShadowsocksTargetAddressTypes(t *testing.T) {
	config := ShadowsocksConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "aes-256-gcm",
		Password: "secret",
	}
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "domain", target: "example.com:443", want: "example.com:443"},
		{name: "ipv4", target: "1.2.3.4:53", want: "1.2.3.4:53"},
		{name: "ipv6", target: "[2001:db8::1]:8443", want: "[2001:db8::1]:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, tt.target))
			got, state := sniffShadowsocksTarget(config, packet)
			if state != sniffFound {
				t.Fatalf("state=%v, want found", state)
			}
			if got != tt.want {
				t.Fatalf("target=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestShadowsocksTCPSnifferWaitsForCompleteFirstChunk(t *testing.T) {
	config := ShadowsocksConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "chacha20-ietf-poly1305",
		Password: "secret",
	}
	packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, "inner.example:443"))
	sniffer := NewShadowsocksTCPSniffer([]ShadowsocksConfig{config}, "203.0.113.10", 39281)
	if sniffer == nil {
		t.Fatal("expected sniffer")
	}
	if target, ok := sniffer.Observe(packet[:10]); ok {
		t.Fatalf("target=%q, want no target from partial packet", target)
	}
	target, ok := sniffer.Observe(packet[10:])
	if !ok {
		t.Fatal("expected target from complete packet")
	}
	if target != "inner.example:443" {
		t.Fatalf("target=%q, want inner.example:443", target)
	}
}

func TestSniffShadowsocks2022Target(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{
			name:     "single psk",
			password: base64.StdEncoding.EncodeToString(testBytes(32, 0x20)),
		},
		{
			name: "eih psk",
			password: base64.StdEncoding.EncodeToString(testBytes(32, 0x40)) + ":" +
				base64.StdEncoding.EncodeToString(testBytes(32, 0x80)),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ShadowsocksConfig{
				Server:   "203.0.113.10",
				Port:     39281,
				Method:   "2022-blake3-aes-256-gcm",
				Password: tt.password,
			}
			packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, "inner.example:443"))
			got, state := sniffShadowsocksTarget(config, packet)
			if state != sniffFound {
				t.Fatalf("state=%v, want found", state)
			}
			if got != "inner.example:443" {
				t.Fatalf("target=%q, want inner.example:443", got)
			}
		})
	}
}

func TestSniffShadowsocks2022UDPRejectsShortEIHPacket(t *testing.T) {
	serverKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x40))
	userKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x80))
	config := ShadowsocksConfig{
		Method:   "2022-blake3-aes-256-gcm",
		Password: serverKey + ":" + userKey,
	}
	spec, ok := shadowsocksCipherSpecForMethod(config.Method)
	if !ok {
		t.Fatal("missing cipher spec")
	}
	keys, ok := parseShadowsocks2022UDPKeys(config.Password, spec.keySize)
	if !ok {
		t.Fatal("expected UDP keys")
	}
	minPacketLen := aes.BlockSize +
		keys.eihLen +
		shadowsocks2022UDPHeaderLen +
		shadowsocksAEADTagSize
	packet := make([]byte, minPacketLen-1)

	if target, ok := sniffShadowsocks2022UDPTargetWithKey(keys, spec, packet); ok {
		t.Fatalf("target=%q, want short packet rejected", target)
	}
}

func TestFreshShadowsocks2022UDPEpoch(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	currentEpoch := unixEpochForTest(t, now)
	tests := []struct {
		name       string
		epochValue uint64
		want       bool
	}{
		{
			name:       "zero epoch",
			epochValue: 0,
			want:       true,
		},
		{
			name:       "current epoch",
			epochValue: currentEpoch,
			want:       true,
		},
		{
			name:       "too old",
			epochValue: currentEpoch - uint64((3 * time.Minute).Seconds()),
			want:       false,
		},
		{
			name:       "too far in future",
			epochValue: currentEpoch + uint64((3 * time.Minute).Seconds()),
			want:       false,
		},
		{
			name:       "int64 overflow",
			epochValue: 1 << 63,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := freshShadowsocks2022UDPEpoch(tt.epochValue, now)
			if got != tt.want {
				t.Fatalf("freshShadowsocks2022UDPEpoch(%d)=%v, want %v", tt.epochValue, got, tt.want)
			}
		})
	}
}

func unixEpochForTest(t *testing.T, at time.Time) uint64 {
	t.Helper()
	epoch := at.Unix()
	if epoch < 0 {
		t.Fatalf("test epoch must be non-negative: %d", epoch)
		return 0
	}
	return uint64(epoch)
}

func TestParseShadowsocks2022UDPKeys(t *testing.T) {
	serverKey := testBytes(32, 0x40)
	userKey := testBytes(32, 0x80)
	password := base64.StdEncoding.EncodeToString(serverKey) + ":" + base64.StdEncoding.EncodeToString(userKey)

	keys, ok := parseShadowsocks2022UDPKeys(password, 32)
	if !ok {
		t.Fatal("expected UDP keys")
	}
	if !bytes.Equal(keys.blockKey, serverKey) {
		t.Fatalf("block key=%x, want %x", keys.blockKey, serverKey)
	}
	if !bytes.Equal(keys.userKey, userKey) {
		t.Fatalf("user key=%x, want %x", keys.userKey, userKey)
	}
	if keys.eihLen != 16 {
		t.Fatalf("eih len=%d, want 16", keys.eihLen)
	}

	keys, ok = parseShadowsocks2022UDPKeys(base64.StdEncoding.EncodeToString(userKey), 32)
	if !ok {
		t.Fatal("expected single-password UDP keys")
	}
	if !bytes.Equal(keys.blockKey, userKey) || !bytes.Equal(keys.userKey, userKey) || keys.eihLen != 0 {
		t.Fatalf("single password keys=%+v, want same block/user key and no EIH", keys)
	}

	if _, ok := parseShadowsocks2022UDPKeys("not-a-valid-psk", 32); ok {
		t.Fatal("invalid PSK was accepted")
	}
}

func TestParseShadowsocks2022TCPKeysUsesLastEIHKey(t *testing.T) {
	userKey := testBytes(32, 0x80)
	keys, ok := parseShadowsocks2022TCPKeys("not-a-valid-block-key:"+base64.StdEncoding.EncodeToString(userKey), 32)
	if !ok {
		t.Fatal("expected keys")
	}
	if !bytes.Equal(keys.userKey, userKey) {
		t.Fatalf("user key=%x, want %x", keys.userKey, userKey)
	}
	if keys.eihLen != 16 {
		t.Fatalf("EIH len=%d, want 16", keys.eihLen)
	}
}

func TestShadowsocksSnifferFallsBackOnFailures(t *testing.T) {
	config := ShadowsocksConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "aes-128-gcm",
		Password: "secret",
	}
	packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, "inner.example:443"))
	wrongPassword := config
	wrongPassword.Password = "wrong"
	if target, state := sniffShadowsocksTarget(wrongPassword, packet); state != sniffFailed || target != "" {
		t.Fatalf("wrong password target=%q state=%v, want failed without target", target, state)
	}

	unsupported := config
	unsupported.Method = "2022-blake3-chacha20-poly1305"
	if sniffer := NewShadowsocksTCPSniffer([]ShadowsocksConfig{unsupported}, unsupported.Server, unsupported.Port); sniffer != nil {
		t.Fatal("expected no sniffer for unsupported method")
	}
	if sniffer := NewShadowsocksTCPSniffer([]ShadowsocksConfig{config}, "203.0.113.1", 39281); sniffer != nil {
		t.Fatal("expected no sniffer for non-matching outer target")
	}
}

func TestShadowsocksUDPSnifferReadsInnerTarget(t *testing.T) {
	// buildShadowsocksUDPPacket encodes this destination into every fixture.
	const wantTarget = "inner.example:5353"

	configs := []ShadowsocksConfig{
		{
			Server:   "203.0.113.10",
			Port:     39281,
			Method:   "aes-256-gcm",
			Password: "secret",
		},
		{
			Server:   "203.0.113.11",
			Port:     39282,
			Method:   "2022-blake3-aes-256-gcm",
			Password: base64.StdEncoding.EncodeToString(testBytes(32, 0x20)),
		},
		{
			// A server:user password carries an EIH block ahead of the ciphertext.
			Server:   "203.0.113.12",
			Port:     39283,
			Method:   "2022-blake3-aes-256-gcm",
			Password: base64.StdEncoding.EncodeToString(testBytes(32, 0x40)) + ":" + base64.StdEncoding.EncodeToString(testBytes(32, 0x80)),
		},
	}
	names := []string{"aead", "2022 single psk", "2022 eih psk"}

	for i, config := range configs {
		t.Run(names[i], func(t *testing.T) {
			packet := buildShadowsocksUDPPacket(t, config, []byte("dns"))

			got, ok := sniffShadowsocksUDPTarget(config, packet)
			if !ok || got != wantTarget {
				t.Fatalf("per-datagram target=%q ok=%v, want %q true", got, ok, wantTarget)
			}

			sniffer := NewShadowsocksUDPSniffer(configs, config.Server, config.Port)
			if sniffer == nil {
				t.Fatal("expected sniffer for matching outer target")
			}
			for attempt := range 3 {
				got, ok := sniffer.Sniff(packet)
				if !ok || got != wantTarget {
					t.Fatalf("cached sniff #%d target=%q ok=%v, want %q true", attempt, got, ok, wantTarget)
				}
			}
		})
	}
}

func TestShadowsocksUDPSnifferSkipsUnmatchedAndUnsupported(t *testing.T) {
	config := ShadowsocksConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "aes-256-gcm",
		Password: "secret",
	}
	if sniffer := NewShadowsocksUDPSniffer([]ShadowsocksConfig{config}, "203.0.113.11", 39281); sniffer != nil {
		t.Fatal("expected no sniffer for non-matching host")
	}
	if sniffer := NewShadowsocksUDPSniffer([]ShadowsocksConfig{config}, "203.0.113.10", 39282); sniffer != nil {
		t.Fatal("expected no sniffer for non-matching port")
	}

	unsupported := config
	unsupported.Method = "2022-blake3-chacha20-poly1305"
	if sniffer := NewShadowsocksUDPSniffer([]ShadowsocksConfig{unsupported}, unsupported.Server, unsupported.Port); sniffer != nil {
		t.Fatal("expected no sniffer for unsupported method")
	}

	badPSK := config
	badPSK.Method = "2022-blake3-aes-256-gcm"
	badPSK.Password = "not-a-32-byte-psk"
	if sniffer := NewShadowsocksUDPSniffer([]ShadowsocksConfig{badPSK}, badPSK.Server, badPSK.Port); sniffer != nil {
		t.Fatal("expected no sniffer for undecodable 2022 PSK")
	}

	var nilSniffer *ShadowsocksUDPSniffer
	if target, ok := nilSniffer.Sniff([]byte("anything")); ok || target != "" {
		t.Fatalf("nil sniffer target=%q ok=%v, want no target", target, ok)
	}
}

func BenchmarkShadowsocksUDPSnifferSniff(b *testing.B) {
	config := ShadowsocksConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "2022-blake3-aes-256-gcm",
		Password: base64.StdEncoding.EncodeToString(testBytes(32, 0x20)),
	}
	configs := []ShadowsocksConfig{config}
	packet := buildShadowsocksUDPPacket(b, config, []byte("dns"))

	b.Run("cached", func(b *testing.B) {
		sniffer := NewShadowsocksUDPSniffer(configs, config.Server, config.Port)
		if sniffer == nil {
			b.Fatal("expected sniffer")
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, ok := sniffer.Sniff(packet); !ok {
				b.Fatal("expected target")
			}
		}
	})
	b.Run("per-datagram", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, ok := sniffShadowsocksUDPTarget(config, packet); !ok {
				b.Fatal("expected target")
			}
		}
	})
}

func BenchmarkShadowsocksSnifferObserve(b *testing.B) {
	config := ShadowsocksConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "aes-256-gcm",
		Password: "secret",
	}
	packet := buildShadowsocksFirstPacket(b, config, encodeShadowsocksTarget(b, "inner.example:443"))
	b.ReportAllocs()
	for b.Loop() {
		sniffer := NewShadowsocksTCPSniffer([]ShadowsocksConfig{config}, "203.0.113.10", 39281)
		if sniffer == nil {
			b.Fatal("expected sniffer")
		}
		target, ok := sniffer.Observe(packet)
		if !ok || target != "inner.example:443" {
			b.Fatalf("target=%q ok=%v", target, ok)
		}
	}
}

func buildShadowsocksFirstPacket(tb testing.TB, config ShadowsocksConfig, payload []byte) []byte {
	tb.Helper()
	spec, ok := shadowsocksCipherSpecForMethod(config.Method)
	if !ok {
		tb.Fatalf("unsupported method %q", config.Method)
	}
	salt := make([]byte, spec.saltSize)
	for i := range salt {
		salt[i] = byte(i + 1)
	}
	subkey, err := deriveShadowsocksSubkey(config.Password, salt, spec.keySize)
	if err != nil {
		tb.Fatal(err)
	}
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		tb.Fatal(err)
	}

	if spec.is2022 {
		return buildShadowsocks2022FirstPacket(tb, config, spec, salt, payload)
	}
	nonce := make([]byte, shadowsocksAEADNonceSize)
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, mustUint16(tb, len(payload), "payload length"))
	packet := append([]byte{}, salt...)
	packet = append(packet, aead.Seal(nil, nonce, length, nil)...)
	incrementShadowsocksNonce(nonce)
	packet = append(packet, aead.Seal(nil, nonce, payload, nil)...)
	return packet
}

func buildShadowsocksUDPPacket(tb testing.TB, config ShadowsocksConfig, payload []byte) []byte {
	tb.Helper()
	spec, ok := shadowsocksCipherSpecForMethod(config.Method)
	if !ok {
		tb.Fatalf("unsupported method %q", config.Method)
	}
	plaintext := encodeShadowsocksTarget(tb, "inner.example:5353")
	plaintext = append(plaintext, payload...)
	if spec.is2022 {
		return buildShadowsocks2022UDPPacket(tb, config, spec, plaintext)
	}
	salt := testBytes(spec.saltSize, 0x31)
	masterKey := evpBytesToKey([]byte(config.Password), spec.keySize)
	subkey, err := deriveShadowsocksSubkeyFromMasterKey(masterKey, salt, spec.keySize)
	if err != nil {
		tb.Fatal(err)
	}
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		tb.Fatal(err)
	}
	packet := append([]byte{}, salt...)
	//nolint:gosec // G407: the first AEAD packet uses the protocol-required zero nonce with its salt-derived key.
	packet = aead.Seal(packet, make([]byte, aead.NonceSize()), plaintext, nil)
	return packet
}

func buildShadowsocks2022UDPPacket(
	tb testing.TB,
	config ShadowsocksConfig,
	spec shadowsocksCipherSpec,
	payload []byte,
) []byte {
	tb.Helper()
	keys, ok := parseShadowsocks2022UDPKeys(config.Password, spec.keySize)
	if !ok {
		tb.Fatal("missing 2022 keys")
	}
	sessionID := uint64(0x0102030405060708)
	packetID := uint64(0x1112131415161718)
	packetHeader := make([]byte, aes.BlockSize)
	binary.BigEndian.PutUint64(packetHeader[:8], sessionID)
	binary.BigEndian.PutUint64(packetHeader[8:], packetID)

	body := []byte{0}
	body = binary.BigEndian.AppendUint64(body, mustCurrentUnixUint64(tb))
	body = binary.BigEndian.AppendUint16(body, 0)
	body = append(body, payload...)

	sessionSalt := packetHeader[:8]
	sessionKey := deriveShadowsocks2022Subkey(keys.userKey, sessionSalt, spec.keySize)
	aead, err := spec.newAEAD(sessionKey)
	if err != nil {
		tb.Fatal(err)
	}
	encryptedBody := aead.Seal(nil, packetHeader[4:16], body, nil)
	block, err := aes.NewCipher(keys.blockKey)
	if err != nil {
		tb.Fatal(err)
	}
	block.Encrypt(packetHeader, packetHeader)
	packet := append([]byte{}, packetHeader...)
	if keys.eihLen > 0 {
		packet = append(packet, testBytes(keys.eihLen, 0xa0)...)
	}
	packet = append(packet, encryptedBody...)
	return packet
}

func buildShadowsocks2022FirstPacket(
	tb testing.TB,
	config ShadowsocksConfig,
	spec shadowsocksCipherSpec,
	salt []byte,
	payload []byte,
) []byte {
	tb.Helper()
	keys, ok := parseShadowsocks2022TCPKeys(config.Password, spec.keySize)
	if !ok {
		tb.Fatal("missing 2022 keys")
	}
	subkey := deriveShadowsocks2022Subkey(keys.userKey, salt, spec.keySize)
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		tb.Fatal(err)
	}

	nonce := make([]byte, shadowsocksAEADNonceSize)
	fixedHeader := make([]byte, shadowsocks2022FixedHeaderLen)
	fixedHeader[0] = 0
	binary.BigEndian.PutUint64(fixedHeader[1:9], mustCurrentUnixUint64(tb))
	binary.BigEndian.PutUint16(fixedHeader[9:11], mustUint16(tb, len(payload), "payload length"))
	packet := append([]byte{}, salt...)
	if keys.eihLen > 0 {
		packet = append(packet, testBytes(keys.eihLen, 0xa0)...)
	}
	packet = append(packet, aead.Seal(nil, nonce, fixedHeader, nil)...)
	incrementShadowsocksNonce(nonce)
	packet = append(packet, aead.Seal(nil, nonce, payload, nil)...)
	return packet
}

func testBytes(size int, start byte) []byte {
	out := make([]byte, size)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}

func encodeShadowsocksTarget(tb testing.TB, target string) []byte {
	tb.Helper()
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		tb.Fatal(err)
	}
	port := mustParseTestPort(tb, portText)
	var payload []byte
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			payload = append(payload, 1)
			payload = append(payload, ip4...)
		} else {
			payload = append(payload, 4)
			payload = append(payload, ip.To16()...)
		}
	} else {
		hostLen := len(host)
		if hostLen > 255 {
			tb.Fatalf("domain too long: %s", host)
			return nil
		}
		payload = append(payload, 3, byte(hostLen))
		payload = append(payload, host...)
	}
	payload = binary.BigEndian.AppendUint16(payload, mustUint16(tb, port, "port"))
	payload = append(payload, []byte("hello")...)
	return payload
}

func mustParseTestPort(tb testing.TB, value string) int {
	tb.Helper()
	port, err := strconv.Atoi(value)
	if err != nil {
		tb.Fatal(err)
	}
	return port
}

func mustUint16(tb testing.TB, value int, label string) uint16 {
	tb.Helper()
	if value < 0 || value > 65535 {
		tb.Fatalf("%s=%d is out of uint16 range", label, value)
		return 0
	}
	return uint16(value)
}

func mustCurrentUnixUint64(tb testing.TB) uint64 {
	tb.Helper()
	now := time.Now().Unix()
	if now < 0 {
		tb.Fatalf("unix time=%d is out of uint64 range", now)
		return 0
	}
	return uint64(now)
}
