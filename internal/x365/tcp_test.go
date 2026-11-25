package x365

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// 1.0.71 shape: the request itself names the destination and is exactly
// 26+len(example.com) bytes before any payload.
const testDirectRequestHex = "58333635010100112233445566778899aabbccddeeff01bb020b6578616d706c652e636f6d"

func TestOpenTCPSendsDirectDestinationAndConsumesResponse(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{}
	stream.read.WriteString("X365\x00")

	conn, err := openTCP(stream, userID, "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	if conn == nil {
		t.Fatal("openTCP returned a nil stream")
	}
	if got := hex.EncodeToString(stream.write.Bytes()); got != testDirectRequestHex {
		t.Fatalf("request hex=%s, want %s", got, testDirectRequestHex)
	}
	if stream.read.Len() != 0 {
		t.Fatalf("%d response bytes left unread, want the five-byte acknowledgement consumed", stream.read.Len())
	}
}

func TestOpenTCPWritesRequestBeforeReadingResponse(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &gatedReadStream{}
	stream.read.WriteString("X365\x00")

	if _, err := openTCP(stream, userID, "example.com", 443); err != nil {
		t.Fatal(err)
	}
}

func TestOpenTCPRelaysRawPayload(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{}
	stream.read.WriteString("X365\x00target-data")

	conn, err := openTCP(stream, userID, "example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "target-data" {
		t.Fatalf("read payload=%q, want target-data", got)
	}

	if _, err := conn.Write([]byte("client-data")); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(stream.write.Bytes(), []byte("client-data")) {
		t.Fatalf("written bytes=%q, want raw client-data after the request", stream.write.Bytes())
	}
}

func TestOpenTCPFailuresCloseStreamAndNameStage(t *testing.T) {
	errBoom := errors.New("write failed")

	tests := []struct {
		name       string
		newStream  func() closeCountingTestStream
		destPort   int
		want       string
		wantDetail string
		wantIs     error
	}{
		{
			name:      "build request",
			newStream: func() closeCountingTestStream { return &memoryStream{} },
			destPort:  -1,
			want:      "build x365 request",
		},
		{
			name:      "write request",
			newStream: func() closeCountingTestStream { return &writeErrorStream{err: errBoom} },
			destPort:  443,
			want:      "write x365 request",
			wantIs:    errBoom,
		},
		{
			name: "read response",
			newStream: func() closeCountingTestStream {
				stream := &memoryStream{}
				stream.read.WriteString("NOPE\x01")
				return stream
			},
			destPort:   443,
			want:       "read x365 response",
			wantDetail: "bad X365 response",
		},
		{
			name: "reject server status",
			newStream: func() closeCountingTestStream {
				stream := &memoryStream{}
				stream.read.WriteString("X365\x01")
				return stream
			},
			destPort:   443,
			want:       "read x365 response",
			wantDetail: "bad X365 response",
		},
	}

	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := tt.newStream()
			conn, err := openTCP(stream, userID, "example.com", tt.destPort)

			if err == nil {
				closeQuietly(conn)
				t.Fatal("openTCP succeeded, want a failure")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want context %q", err, tt.want)
			}
			if tt.wantDetail != "" && !strings.Contains(err.Error(), tt.wantDetail) {
				t.Fatalf("err=%v, want detail %q", err, tt.wantDetail)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("err=%v, want %v in the chain", err, tt.wantIs)
			}
			if got := stream.closes(); got != 1 {
				t.Fatalf("stream close count=%d, want 1", got)
			}
		})
	}
}

func TestClientOpenTCPWrapsTransportError(t *testing.T) {
	client := &Client{}
	_, err := client.OpenTCP(t.Context(), "example.org", 443)
	if err == nil {
		t.Fatal("expected transport setup error")
	}
	if !strings.Contains(err.Error(), "open xhttp reality stream") {
		t.Fatalf("err=%v, want transport context", err)
	}
	if !strings.Contains(err.Error(), "node path is empty") {
		t.Fatalf("err=%v, want original node path error", err)
	}
}

// gatedReadStream fails if the opening code reads the response before writing
// the request.
type gatedReadStream struct {
	memoryStream
}

func (s *gatedReadStream) Read(p []byte) (int, error) {
	if s.write.Len() == 0 {
		return 0, errors.New("read before request write")
	}
	return s.memoryStream.Read(p)
}
