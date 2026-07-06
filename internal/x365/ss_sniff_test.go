package x365

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
	config := ShadowsocksSniffConfig{
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

func TestShadowsocksTargetSnifferWaitsForCompleteFirstChunk(t *testing.T) {
	config := ShadowsocksSniffConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "chacha20-ietf-poly1305",
		Password: "secret",
	}
	packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, "inner.example:443"))
	sniffer := NewShadowsocksTargetSniffer([]ShadowsocksSniffConfig{config}, "203.0.113.10", 39281)
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
	config := ShadowsocksSniffConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "2022-blake3-aes-256-gcm",
		Password: base64.StdEncoding.EncodeToString(testBytes(32, 0x20)),
	}
	packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, "inner.example:443"))
	got, state := sniffShadowsocksTarget(config, packet)
	if state != sniffFound {
		t.Fatalf("state=%v, want found", state)
	}
	if got != "inner.example:443" {
		t.Fatalf("target=%q, want inner.example:443", got)
	}
}

func TestSniffShadowsocks2022TargetWithEIHPassword(t *testing.T) {
	serverKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x40))
	userKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x80))
	config := ShadowsocksSniffConfig{
		Method:   "2022-blake3-aes-256-gcm",
		Password: serverKey + ":" + userKey,
	}
	packet := buildShadowsocksFirstPacket(t, config, encodeShadowsocksTarget(t, "inner.example:443"))
	got, state := sniffShadowsocksTarget(config, packet)
	if state != sniffFound {
		t.Fatalf("state=%v, want found", state)
	}
	if got != "inner.example:443" {
		t.Fatalf("target=%q, want inner.example:443", got)
	}
}

func TestSniffShadowsocksUDPTarget(t *testing.T) {
	config := ShadowsocksSniffConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "aes-256-gcm",
		Password: "secret",
	}
	packet := buildShadowsocksUDPPacket(t, config, []byte("dns"))
	got, ok := sniffShadowsocksUDPTarget(config, packet)
	if !ok {
		t.Fatal("expected UDP target")
	}
	if got != "inner.example:5353" {
		t.Fatalf("target=%q, want inner.example:5353", got)
	}
}

func TestSniffShadowsocks2022UDPTarget(t *testing.T) {
	config := ShadowsocksSniffConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "2022-blake3-aes-256-gcm",
		Password: base64.StdEncoding.EncodeToString(testBytes(32, 0x20)),
	}
	packet := buildShadowsocksUDPPacket(t, config, []byte("dns"))
	got, ok := sniffShadowsocksUDPTarget(config, packet)
	if !ok {
		t.Fatal("expected UDP target")
	}
	if got != "inner.example:5353" {
		t.Fatalf("target=%q, want inner.example:5353", got)
	}
}

func TestSniffShadowsocks2022UDPWithEIHPassword(t *testing.T) {
	serverKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x40))
	userKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x80))
	config := ShadowsocksSniffConfig{
		Method:   "2022-blake3-aes-256-gcm",
		Password: serverKey + ":" + userKey,
	}
	packet := buildShadowsocksUDPPacket(t, config, []byte("dns"))
	got, ok := sniffShadowsocksUDPTarget(config, packet)
	if !ok {
		t.Fatal("expected UDP target")
	}
	if got != "inner.example:5353" {
		t.Fatalf("target=%q, want inner.example:5353", got)
	}
}

func TestSniffShadowsocks2022UDPRejectsShortEIHPacket(t *testing.T) {
	serverKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x40))
	userKey := base64.StdEncoding.EncodeToString(testBytes(32, 0x80))
	config := ShadowsocksSniffConfig{
		Method:   "2022-blake3-aes-256-gcm",
		Password: serverKey + ":" + userKey,
	}
	spec, ok := ssCipherSpecForMethod(config.Method)
	if !ok {
		t.Fatal("missing cipher spec")
	}
	candidate, ok := shadowsocks2022UDPKeyCandidateFor(config.Password, spec.keySize)
	if !ok {
		t.Fatal("expected UDP key candidate")
	}
	minPacketLen := aes.BlockSize +
		candidate.eihLen +
		shadowsocks2022UDPHeaderLen +
		shadowsocksAEADTagSize
	packet := make([]byte, minPacketLen-1)

	if target, ok := sniffShadowsocks2022UDPTargetWithKey(candidate, spec, packet); ok {
		t.Fatalf("target=%q, want short packet rejected", target)
	}
}

func TestNormalizeShadowsocksSniffConfigs(t *testing.T) {
	got, err := NormalizeShadowsocksSniffConfigs([]ShadowsocksSniffConfig{{
		Name:     " landing ",
		Server:   " 203.0.113.10 ",
		Port:     39281,
		Cipher:   " AES-256-GCM ",
		Password: "secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len(configs)=%d, want 1", len(got))
	}
	if got[0].Name != "landing" || got[0].Server != "203.0.113.10" {
		t.Fatalf("config trim result=%+v", got[0])
	}
	if got[0].Method != "aes-256-gcm" || got[0].Cipher != "" {
		t.Fatalf("method=%q cipher=%q, want normalized method and empty cipher", got[0].Method, got[0].Cipher)
	}
}

func TestNormalizeShadowsocksSniffConfigsRejectsInvalidConfig(t *testing.T) {
	_, err := NormalizeShadowsocksSniffConfigs([]ShadowsocksSniffConfig{{
		Server: "203.0.113.10",
		Port:   70000,
		Method: "aes-256-gcm",
	}})
	if err == nil {
		t.Fatal("expected invalid sniff config error")
	}
}

func TestFormatTargetChain(t *testing.T) {
	if got := FormatTargetChain("outer.example:443", "inner.example:443"); got != "outer.example:443->inner.example:443" {
		t.Fatalf("target chain=%q, want outer->inner", got)
	}
	if got := FormatTargetChain("outer.example:443", ""); got != "outer.example:443" {
		t.Fatalf("empty inner chain=%q, want outer", got)
	}
	if got := FormatTargetChain("outer.example:443", "outer.example:443"); got != "outer.example:443" {
		t.Fatalf("same inner chain=%q, want outer", got)
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

func TestShadowsocks2022UDPKeyCandidateFor(t *testing.T) {
	serverKey := testBytes(32, 0x40)
	userKey := testBytes(32, 0x80)
	password := base64.StdEncoding.EncodeToString(serverKey) + ":" + base64.StdEncoding.EncodeToString(userKey)

	candidate, ok := shadowsocks2022UDPKeyCandidateFor(password, 32)
	if !ok {
		t.Fatal("expected UDP key candidate")
	}
	if !bytes.Equal(candidate.blockKey, serverKey) {
		t.Fatalf("block key=%x, want %x", candidate.blockKey, serverKey)
	}
	if !bytes.Equal(candidate.userKey, userKey) {
		t.Fatalf("user key=%x, want %x", candidate.userKey, userKey)
	}
	if candidate.eihLen != 16 {
		t.Fatalf("eih len=%d, want 16", candidate.eihLen)
	}

	candidate, ok = shadowsocks2022UDPKeyCandidateFor(base64.StdEncoding.EncodeToString(userKey), 32)
	if !ok {
		t.Fatal("expected single-password UDP key candidate")
	}
	if !bytes.Equal(candidate.blockKey, userKey) || !bytes.Equal(candidate.userKey, userKey) || candidate.eihLen != 0 {
		t.Fatalf("single password candidate=%+v, want same block/user key and no EIH", candidate)
	}

	if _, ok := shadowsocks2022UDPKeyCandidateFor("not-a-valid-psk", 32); ok {
		t.Fatal("invalid PSK was accepted")
	}
}

func TestShadowsocks2022KeyCandidateUsesLastEIHKey(t *testing.T) {
	userKey := testBytes(32, 0x80)
	candidate, ok := shadowsocks2022KeyCandidateFor("not-a-valid-block-key:"+base64.StdEncoding.EncodeToString(userKey), 32)
	if !ok {
		t.Fatal("expected key candidate")
	}
	if !bytes.Equal(candidate.userKey, userKey) {
		t.Fatalf("candidate key=%x, want %x", candidate.userKey, userKey)
	}
	if candidate.eihLen != 16 {
		t.Fatalf("candidate EIH len=%d, want 16", candidate.eihLen)
	}
}

func TestShadowsocksSnifferFallsBackOnFailures(t *testing.T) {
	config := ShadowsocksSniffConfig{
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
	if limit := shadowsocksSniffBufferLimit(unsupported.Method); limit != 0 {
		t.Fatalf("unsupported limit=%d, want 0", limit)
	}
	if sniffer := NewShadowsocksTargetSniffer([]ShadowsocksSniffConfig{config}, "203.0.113.1", 39281); sniffer != nil {
		t.Fatal("expected no sniffer for non-matching outer target")
	}
}

func BenchmarkShadowsocksSnifferObserve(b *testing.B) {
	config := ShadowsocksSniffConfig{
		Server:   "203.0.113.10",
		Port:     39281,
		Method:   "aes-256-gcm",
		Password: "secret",
	}
	packet := buildShadowsocksFirstPacket(b, config, encodeShadowsocksTarget(b, "inner.example:443"))
	b.ReportAllocs()
	for b.Loop() {
		sniffer := NewShadowsocksTargetSniffer([]ShadowsocksSniffConfig{config}, "203.0.113.10", 39281)
		if sniffer == nil {
			b.Fatal("expected sniffer")
		}
		target, ok := sniffer.Observe(packet)
		if !ok || target != "inner.example:443" {
			b.Fatalf("target=%q ok=%v", target, ok)
		}
	}
}

func buildShadowsocksFirstPacket(tb testing.TB, config ShadowsocksSniffConfig, payload []byte) []byte {
	tb.Helper()
	spec, ok := ssCipherSpecForMethod(config.Method)
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

func buildShadowsocksUDPPacket(tb testing.TB, config ShadowsocksSniffConfig, payload []byte) []byte {
	tb.Helper()
	spec, ok := ssCipherSpecForMethod(config.Method)
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
	packet = aead.Seal(packet, make([]byte, aead.NonceSize()), plaintext, nil)
	return packet
}

func buildShadowsocks2022UDPPacket(
	tb testing.TB,
	config ShadowsocksSniffConfig,
	spec ssCipherSpec,
	payload []byte,
) []byte {
	tb.Helper()
	candidate, ok := shadowsocks2022UDPKeyCandidateFor(config.Password, spec.keySize)
	if !ok {
		tb.Fatal("missing 2022 key candidate")
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
	sessionKey := deriveShadowsocks2022Subkey(candidate.userKey, sessionSalt, spec.keySize)
	aead, err := spec.newAEAD(sessionKey)
	if err != nil {
		tb.Fatal(err)
	}
	encryptedBody := aead.Seal(nil, packetHeader[4:16], body, nil)
	block, err := aes.NewCipher(candidate.blockKey)
	if err != nil {
		tb.Fatal(err)
	}
	block.Encrypt(packetHeader, packetHeader)
	packet := append([]byte{}, packetHeader...)
	if candidate.eihLen > 0 {
		packet = append(packet, testBytes(candidate.eihLen, 0xa0)...)
	}
	packet = append(packet, encryptedBody...)
	return packet
}

func buildShadowsocks2022FirstPacket(
	tb testing.TB,
	config ShadowsocksSniffConfig,
	spec ssCipherSpec,
	salt []byte,
	payload []byte,
) []byte {
	tb.Helper()
	candidate, ok := shadowsocks2022KeyCandidateFor(config.Password, spec.keySize)
	if !ok {
		tb.Fatal("missing 2022 key candidate")
	}
	subkey := deriveShadowsocks2022Subkey(candidate.userKey, salt, spec.keySize)
	aead, err := spec.newAEAD(subkey)
	if err != nil {
		tb.Fatal(err)
	}

	nonce := make([]byte, shadowsocksAEADNonceSize)
	fixedHeader := make([]byte, shadowsocks2022FixedHeader)
	fixedHeader[0] = 0
	binary.BigEndian.PutUint64(fixedHeader[1:9], mustCurrentUnixUint64(tb))
	binary.BigEndian.PutUint16(fixedHeader[9:11], mustUint16(tb, len(payload), "payload length"))
	packet := append([]byte{}, salt...)
	if candidate.eihLen > 0 {
		packet = append(packet, testBytes(candidate.eihLen, 0xa0)...)
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
