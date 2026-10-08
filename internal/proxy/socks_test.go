package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"testing/iotest"
)

func TestSOCKS5PipelinedConnect(t *testing.T) {
	for _, fragmented := range []bool{false, true} {
		t.Run(fmt.Sprintf("fragmented=%t", fragmented), func(t *testing.T) {
			input := append([]byte{5, 3, 2, 0, 1, 5, 1, 0, 3, 11}, []byte("example.org")...)
			input = append(input, 1, 187)
			payload := []byte("first application bytes")
			input = append(input, payload...)
			var source io.Reader = bytes.NewReader(input)
			if fragmented {
				source = iotest.OneByteReader(source)
			}
			reader := bufio.NewReader(source)
			var output bytes.Buffer
			if err := handshakeSOCKS(reader, &output); err != nil {
				t.Fatal(err)
			}
			host, port, err := readSOCKSConnect(reader, &output)
			if err != nil {
				t.Fatal(err)
			}
			if host != "example.org" || port != 443 {
				t.Fatalf("got %s:%d, want example.org:443", host, port)
			}
			if !bytes.Equal(output.Bytes(), []byte{5, 0}) {
				t.Fatalf("bad handshake response: %x", output.Bytes())
			}
			remaining, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(remaining, payload) {
				t.Fatalf("remaining=%q err=%v, want application payload %q", remaining, err, payload)
			}
		})
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
			var output bytes.Buffer
			reader := bufio.NewReader(iotest.OneByteReader(bytes.NewReader(tt.req)))
			got, err := readSOCKSRequest(reader, &output)
			if err != nil {
				t.Fatal(err)
			}
			want := socksRequest{Command: socksCommandConnect, Host: tt.host, Port: tt.port}
			if got != want {
				t.Fatalf("request=%+v, want %+v", got, want)
			}
			if address := got.address(); address != net.JoinHostPort(tt.host, fmt.Sprint(tt.port)) {
				t.Fatalf("address=%q for host=%q port=%d", address, tt.host, tt.port)
			}
			if output.Len() != 0 {
				t.Fatalf("parser replied before upstream open: %x", output.Bytes())
			}
			for end := range len(tt.req) {
				t.Run(fmt.Sprintf("truncated_at_%d", end), func(t *testing.T) {
					var reply bytes.Buffer
					_, err := readSOCKSRequest(bufio.NewReader(bytes.NewReader(tt.req[:end])), &reply)
					if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Fatalf("err=%v, want EOF or unexpected EOF", err)
					}
					if reply.Len() != 0 {
						t.Fatalf("truncated request produced a reply: %x", reply.Bytes())
					}
				})
			}
		})
	}
}

func TestSOCKS5RequestRejectsBadRequests(t *testing.T) {
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
		{
			name:      "unsupported address type",
			req:       []byte{5, 1, 0, 9, 127, 0, 0, 1, 0x01, 0xbb},
			wantReply: []byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0},
			wantErr:   "unsupported SOCKS5 address type: 9",
		},
		{
			name:      "bind command",
			req:       []byte{5, socksCommandBind, 0, 1, 127, 0, 0, 1, 0x01, 0xbb},
			wantReply: []byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0},
			wantErr:   "unsupported SOCKS5 command: BIND(2)",
		},
		{
			name:      "unknown command",
			req:       []byte{5, 9, 0, 1, 127, 0, 0, 1, 0x01, 0xbb},
			wantReply: []byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0},
			wantErr:   "unsupported SOCKS5 command: UNKNOWN(9)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			_, err := readSOCKSRequest(bufio.NewReader(bytes.NewReader(tt.req)), &output)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tt.wantErr)
			}
			if !bytes.Equal(output.Bytes(), tt.wantReply) {
				t.Fatalf("reply=%x, want %x", output.Bytes(), tt.wantReply)
			}
		})
	}
}

func TestSOCKS5HandshakeRejectsInvalidGreetings(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		wantReply []byte
		wantErr   error
	}{
		{name: "missing greeting", wantErr: io.EOF},
		{name: "missing method count", input: []byte{5}, wantErr: io.ErrUnexpectedEOF},
		{name: "wrong version", input: []byte{4, 1, 0}},
		{name: "no methods", input: []byte{5, 0}, wantReply: []byte{5, 0xff}},
		{name: "missing methods", input: []byte{5, 1}, wantErr: io.EOF},
		{name: "truncated methods", input: []byte{5, 2, 0}, wantErr: io.ErrUnexpectedEOF},
		{name: "unsupported methods", input: []byte{5, 2, 1, 2}, wantReply: []byte{5, 0xff}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := handshakeSOCKS(bufio.NewReader(bytes.NewReader(tt.input)), &output)
			if err == nil || (tt.wantErr != nil && !errors.Is(err, tt.wantErr)) {
				t.Fatalf("err=%v, want rejection with cause %v", err, tt.wantErr)
			}
			if !bytes.Equal(output.Bytes(), tt.wantReply) {
				t.Fatalf("reply=%x, want %x", output.Bytes(), tt.wantReply)
			}
		})
	}
}

func TestSOCKS5UDPDatagramWireFormat(t *testing.T) {
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
			got, err := parseSOCKSUDPDatagram(tt.packet)
			if err != nil {
				t.Fatal(err)
			}
			if got.Frag != 0 || got.Host != tt.host || got.Port != tt.port || !bytes.Equal(got.Payload, tt.payload) {
				t.Fatalf("got %+v, want host=%s port=%d payload=%x", got, tt.host, tt.port, tt.payload)
			}
			packet, err := buildSOCKSUDPDatagram(tt.host, tt.port, tt.payload)
			if err != nil || !bytes.Equal(packet, tt.packet) {
				t.Fatalf("encoded packet=%x err=%v, want %x", packet, err, tt.packet)
			}
			headerEnd := len(tt.packet) - len(tt.payload)
			header, err := buildSOCKSUDPHeader(tt.host, tt.port)
			if err != nil || !bytes.Equal(header, tt.packet[:headerEnd]) {
				t.Fatalf("encoded header=%x err=%v, want %x", header, err, tt.packet[:headerEnd])
			}
			// Empty UDP payloads are valid, but every truncated envelope must fail.
			empty, err := parseSOCKSUDPDatagram(tt.packet[:headerEnd])
			if err != nil || empty.Host != tt.host || empty.Port != tt.port || len(empty.Payload) != 0 {
				t.Fatalf("empty datagram=%+v err=%v", empty, err)
			}
			for end := range headerEnd {
				if _, err := parseSOCKSUDPDatagram(tt.packet[:end]); err == nil {
					t.Fatalf("accepted envelope truncated at byte %d", end)
				}
			}
		})
	}
}

func TestAppendSOCKSAddressRejectsInvalidAddress(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{name: "negative port", host: "example.com", port: -1},
		{name: "port overflow", host: "example.com", port: 65536},
		{name: "domain too long", host: strings.Repeat("a", 256), port: 53},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := appendSOCKSAddress(nil, tt.host, tt.port); err == nil {
				t.Fatalf("appendSOCKSAddress(%q, %d) succeeded, want error", tt.host, tt.port)
			}
			if _, err := buildSOCKSUDPDatagram(tt.host, tt.port, nil); err == nil {
				t.Fatalf("buildSOCKSUDPDatagram(%q, %d) succeeded, want error", tt.host, tt.port)
			}
		})
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
			if err := writeSOCKSReplyAddr(&output, 0, tt.addr); err != nil {
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

	got, err := parseSOCKSUDPDatagram(packet)
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
	got, err := parseSOCKSUDPDatagram(packet)
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
			if _, err := parseSOCKSUDPDatagram(tt.packet); err == nil {
				t.Fatalf("parseSOCKSUDPDatagram(%x) succeeded, want error", tt.packet)
			}
		})
	}
}

func TestSOCKS5UDPAssociateRequestIsParsed(t *testing.T) {
	request := []byte{5, socksCommandUDPAssociate, 0, 1, 0, 0, 0, 0, 0, 0}
	got, err := readSOCKSRequest(bufio.NewReader(bytes.NewReader(request)), bytes.NewBuffer(nil))
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
	packet, err := buildSOCKSUDPDatagram("example.com", 53, payload)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		got, err := parseSOCKSUDPDatagram(packet)
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
		packet, err := buildSOCKSUDPDatagram("example.com", 53, payload)
		if err != nil {
			b.Fatal(err)
		}
		if len(packet) != 1218 {
			b.Fatalf("packet len=%d, want 1218", len(packet))
		}
	}
}
