package proxy

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestSOCKS5PipelinedConnect(t *testing.T) {
	var input bytes.Buffer
	input.Write([]byte{5, 1, 0})
	input.Write([]byte{5, 1, 0, 3, 11})
	input.WriteString("example.com")
	input.Write([]byte{1, 187})

	var output bytes.Buffer
	reader := bufio.NewReader(&input)
	if err := socksHandshake(reader, &output); err != nil {
		t.Fatal(err)
	}
	host, port, err := socksReadConnect(reader, &output)
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != 443 {
		t.Fatalf("got %s:%d, want example.com:443", host, port)
	}
	if !bytes.Equal(output.Bytes(), []byte{5, 0}) {
		t.Fatalf("bad handshake response: %x", output.Bytes())
	}
}

func TestSOCKS5ConnectAddressTypes(t *testing.T) {
	tests := []struct {
		name string
		req  []byte
		host string
		port int
	}{
		{
			name: "ipv4",
			req:  []byte{5, 1, 0, 1, 127, 0, 0, 1, 0x1f, 0x90},
			host: "127.0.0.1",
			port: 8080,
		},
		{
			name: "domain",
			req:  append(append([]byte{5, 1, 0, 3, 11}, []byte("example.com")...), 0x01, 0xbb),
			host: "example.com",
			port: 443,
		},
		{
			name: "ipv6",
			req:  append([]byte{5, 1, 0, 4}, append(net.ParseIP("2001:db8::1").To16(), 0x01, 0xbb)...),
			host: "2001:db8::1",
			port: 443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(bytes.NewReader(tt.req))
			host, port, err := socksReadConnect(reader, bytes.NewBuffer(nil))
			if err != nil {
				t.Fatal(err)
			}
			if host != tt.host || port != tt.port {
				t.Fatalf("got %s:%d, want %s:%d", host, port, tt.host, tt.port)
			}
		})
	}
}

func TestSOCKS5RequestRejectsMalformedAddress(t *testing.T) {
	tests := []struct {
		name      string
		req       []byte
		wantReply []byte
		wantErr   string
	}{
		{
			name:      "reserved byte is not zero",
			req:       []byte{5, 1, 9, 1, 127, 0, 0, 1, 0x1f, 0x90},
			wantReply: []byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0},
			wantErr:   "reserved byte",
		},
		{
			name:      "empty domain",
			req:       []byte{5, 1, 0, 3, 0, 0x01, 0xbb},
			wantReply: []byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0},
			wantErr:   "empty SOCKS5 domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			_, _, err := socksReadConnect(bufio.NewReader(bytes.NewReader(tt.req)), &output)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tt.wantErr)
			}
			if !bytes.Equal(output.Bytes(), tt.wantReply) {
				t.Fatalf("reply=%x, want %x", output.Bytes(), tt.wantReply)
			}
		})
	}
}

func TestSOCKS5UnsupportedAuthMethod(t *testing.T) {
	input := bytes.NewReader([]byte{5, 1, 2})
	var output bytes.Buffer
	err := socksHandshake(bufio.NewReader(input), &output)
	if err == nil || !strings.Contains(err.Error(), "no-auth") {
		t.Fatalf("expected no-auth error, got %v", err)
	}
	if !bytes.Equal(output.Bytes(), []byte{5, 0xff}) {
		t.Fatalf("bad response: %x", output.Bytes())
	}
}

func TestParseSOCKS5UDPDatagramAddressTypes(t *testing.T) {
	tests := []struct {
		name    string
		packet  []byte
		host    string
		port    int
		payload []byte
	}{
		{
			name:    "ipv4",
			packet:  []byte{0, 0, 0, 1, 127, 0, 0, 1, 0x00, 0x35, 1, 2, 3},
			host:    "127.0.0.1",
			port:    53,
			payload: []byte{1, 2, 3},
		},
		{
			name:    "domain",
			packet:  append(append([]byte{0, 0, 0, 3, 11}, []byte("example.com")...), 0x01, 0xbb, 4, 5),
			host:    "example.com",
			port:    443,
			payload: []byte{4, 5},
		},
		{
			name:    "ipv6",
			packet:  append([]byte{0, 0, 0, 4}, append(net.ParseIP("2001:db8::1").To16(), 0x01, 0xbb, 6)...),
			host:    "2001:db8::1",
			port:    443,
			payload: []byte{6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSocksUDPDatagram(tt.packet)
			if err != nil {
				t.Fatal(err)
			}
			if got.Frag != 0 || got.Host != tt.host || got.Port != tt.port || !bytes.Equal(got.Payload, tt.payload) {
				t.Fatalf("got %+v, want host=%s port=%d payload=%x", got, tt.host, tt.port, tt.payload)
			}
		})
	}
}

func TestBuildSOCKS5UDPDatagramRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{name: "domain", host: "example.com", port: 53},
		{name: "ipv4", host: "127.0.0.1", port: 5353},
		{name: "ipv6", host: "2001:db8::1", port: 853},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet, err := buildSocksUDPDatagram(tt.host, tt.port, []byte{0xaa, 0xbb})
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseSocksUDPDatagram(packet)
			if err != nil {
				t.Fatal(err)
			}
			if got.Host != tt.host || got.Port != tt.port || !bytes.Equal(got.Payload, []byte{0xaa, 0xbb}) {
				t.Fatalf("bad datagram: %+v", got)
			}
		})
	}
}

func TestBuildSOCKS5UDPHeaderMatchesDatagramEnvelope(t *testing.T) {
	payload := []byte{0xaa, 0xbb}
	header, err := buildSocksUDPHeader("example.com", 53)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := buildSocksUDPDatagram("example.com", 53, payload)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte(nil), header...), payload...)
	if !bytes.Equal(packet, want) {
		t.Fatalf("packet=%x, want header+payload %x", packet, want)
	}
}

func TestAppendSOCKSAddressEncodesAddressTypes(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{name: "ipv4", host: "127.0.0.1", port: 53},
		{name: "domain", host: "example.com", port: 443},
		{name: "ipv6", host: "2001:db8::1", port: 853},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := make([]byte, 0, 4+len(tt.host)+2+1)
			packet = append(packet, 0, 0, 0)
			packet, err := appendSocksAddress(packet, tt.host, tt.port)
			if err != nil {
				t.Fatal(err)
			}
			packet = append(packet, 0xaa)

			got, err := parseSocksUDPDatagram(packet)
			if err != nil {
				t.Fatal(err)
			}
			if got.Host != tt.host || got.Port != tt.port || !bytes.Equal(got.Payload, []byte{0xaa}) {
				t.Fatalf("datagram=%+v, want host=%s port=%d payload=aa", got, tt.host, tt.port)
			}
		})
	}
}

func TestAppendSOCKSAddressRejectsInvalidAddress(t *testing.T) {
	if _, err := appendSocksAddress(nil, "example.com", -1); err == nil {
		t.Fatal("expected negative port error")
	}
	if _, err := appendSocksAddress(nil, "example.com", 65536); err == nil {
		t.Fatal("expected port overflow error")
	}
	if _, err := appendSocksAddress(nil, strings.Repeat("a", 256), 53); err == nil {
		t.Fatal("expected domain length error")
	}
}

func TestWriteSOCKSReplyAddrEncodesBindAddress(t *testing.T) {
	tests := []struct {
		name string
		addr net.Addr
		want []byte
	}{
		{
			name: "ipv4",
			addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10865},
			want: []byte{5, 0, 0, 1, 127, 0, 0, 1, 0x2a, 0x71},
		},
		{
			name: "ipv6",
			addr: &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443},
			want: append([]byte{5, 0, 0, 4}, append(net.ParseIP("2001:db8::1").To16(), 0x01, 0xbb)...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := writeSocksReplyAddr(&output, 0, tt.addr); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(output.Bytes(), tt.want) {
				t.Fatalf("reply=%x, want %x", output.Bytes(), tt.want)
			}
		})
	}
}

func TestParseSOCKS5UDPDatagramPayloadSharesPacketStorage(t *testing.T) {
	packet := make([]byte, 0, 20)
	packet = append(packet, 0, 0, 0, 3, 11)
	packet = append(packet, []byte("example.com")...)
	packet = append(packet, 0x00, 0x35, 0xaa, 0xbb)

	got, err := parseSocksUDPDatagram(packet)
	if err != nil {
		t.Fatal(err)
	}
	packet[len(packet)-1] = 0xcc
	if !bytes.Equal(got.Payload, []byte{0xaa, 0xcc}) {
		t.Fatalf("payload=%x, want alias of caller-owned packet payload", got.Payload)
	}
}

func TestParseSOCKS5UDPDatagramKeepsFragmentNumber(t *testing.T) {
	packet := []byte{0, 0, 2, 1, 127, 0, 0, 1, 0x00, 0x35}
	got, err := parseSocksUDPDatagram(packet)
	if err != nil {
		t.Fatal(err)
	}
	if got.Frag != 2 {
		t.Fatalf("frag=%d, want 2", got.Frag)
	}
}

func TestSOCKS5UDPDatagramRejectsBadPackets(t *testing.T) {
	tests := []struct {
		name   string
		packet []byte
	}{
		{
			name:   "reserved bytes are not zero",
			packet: []byte{0, 1, 0, 1, 127, 0, 0, 1, 0, 53},
		},
		{
			name:   "unsupported address type",
			packet: []byte{0, 0, 0, 9, 127, 0, 0, 1, 0, 53},
		},
		{
			name:   "truncated ipv4",
			packet: []byte{0, 0, 0, 1, 127},
		},
		{
			name:   "truncated domain",
			packet: []byte{0, 0, 0, 3, 11, 'e', 'x'},
		},
		{
			name:   "empty domain",
			packet: []byte{0, 0, 0, 3, 0, 0, 53},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseSocksUDPDatagram(tt.packet); err == nil {
				t.Fatalf("parseSocksUDPDatagram(%x) succeeded, want error", tt.packet)
			}
		})
	}
	if _, err := buildSocksUDPDatagram("example.com", 70000, nil); err == nil {
		t.Fatal("expected port range error")
	}
}

func TestSOCKS5UDPAssociateIsReported(t *testing.T) {
	var input bytes.Buffer
	input.Write([]byte{5, 1, 0})
	input.Write([]byte{5, 3, 0, 1, 127, 0, 0, 1, 0x1f, 0x90})

	var output bytes.Buffer
	reader := bufio.NewReader(&input)
	if err := socksHandshake(reader, &output); err != nil {
		t.Fatal(err)
	}
	_, _, err := socksReadConnect(reader, &output)
	if err == nil || !strings.Contains(err.Error(), "UDP_ASSOCIATE") {
		t.Fatalf("expected UDP_ASSOCIATE error, got %v", err)
	}
	if !bytes.Equal(output.Bytes(), []byte{5, 0, 5, 7, 0, 1, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("bad response: %x", output.Bytes())
	}
}

func TestSOCKS5UDPAssociateRequestIsParsed(t *testing.T) {
	request := []byte{5, socksCommandUDPAssociate, 0, 1, 0, 0, 0, 0, 0, 0}
	got, err := socksReadRequest(bufio.NewReader(bytes.NewReader(request)), bytes.NewBuffer(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != socksCommandUDPAssociate || got.Host != "0.0.0.0" || got.Port != 0 {
		t.Fatalf("request=%+v", got)
	}
}

func TestFakeIPHostDetection(t *testing.T) {
	for _, host := range []string{"198.18.0.1", "198.19.255.255"} {
		if !isFakeIPHost(host) {
			t.Fatalf("expected %s to be detected as fake-ip", host)
		}
	}
	for _, host := range []string{"198.17.255.255", "198.20.0.1", "ifconfig.me"} {
		if isFakeIPHost(host) {
			t.Fatalf("expected %s not to be detected as fake-ip", host)
		}
	}
}

func BenchmarkParseSOCKS5UDPDatagram(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1200)
	packet, err := buildSocksUDPDatagram("example.com", 53, payload)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		got, err := parseSocksUDPDatagram(packet)
		if err != nil {
			b.Fatal(err)
		}
		if got.Host != "example.com" || got.Port != 53 || len(got.Payload) != len(payload) {
			b.Fatalf("got host=%s port=%d payload=%d", got.Host, got.Port, len(got.Payload))
		}
	}
}

func BenchmarkBuildSOCKS5UDPDatagram(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 1200)

	b.ReportAllocs()
	for b.Loop() {
		packet, err := buildSocksUDPDatagram("example.com", 53, payload)
		if err != nil {
			b.Fatal(err)
		}
		if len(packet) != 1218 {
			b.Fatalf("packet len=%d, want 1218", len(packet))
		}
	}
}
