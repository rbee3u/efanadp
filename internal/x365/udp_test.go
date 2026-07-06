package x365

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestOpenUDPSendsCommandTwoRequest(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{}
	conn, err := openUDP(stream, userID, "example.com", 53)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = conn.Close()
	}()

	request := stream.write.Bytes()
	if len(request) < 6 {
		t.Fatalf("request too short: %x", request)
	}
	if request[5] != byte(x365CommandUDP) {
		t.Fatalf("command byte=%d, want %d", request[5], x365CommandUDP)
	}
}

func TestOpenUDPSetupErrorsCloseAndWrap(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}

	stream := &memoryStream{}
	_, err = openUDP(stream, userID, "example.com", -1)
	if err == nil || !strings.Contains(err.Error(), "build x365 udp request") {
		t.Fatalf("err=%v, want UDP request build context", err)
	}
	if stream.closeCount != 1 {
		t.Fatalf("stream close count=%d, want 1", stream.closeCount)
	}

	errBoom := errors.New("write failed")
	writeStream := &writeErrorStream{err: errBoom}
	_, err = openUDP(writeStream, userID, "example.com", 53)
	if !errors.Is(err, errBoom) {
		t.Fatalf("err=%v, want write failure in chain", err)
	}
	if !strings.Contains(err.Error(), "write x365 udp request") {
		t.Fatalf("err=%v, want UDP write context", err)
	}
	if writeStream.closeCount != 1 {
		t.Fatalf("write stream close count=%d, want 1", writeStream.closeCount)
	}
}

func TestOpenX365UDPWrapsTransportError(t *testing.T) {
	_, err := openX365UDP(t.Context(), Node{}, UserID{}, 0, "example.com", 53)
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

func TestX365UDPConnFramesWrites(t *testing.T) {
	stream := &memoryStream{}
	conn := &x365UDPConn{stream: stream}
	n, err := conn.Write([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("n=%d, want 3", n)
	}
	if got, want := stream.write.Bytes(), []byte{0, 3, 'a', 'b', 'c'}; !bytes.Equal(got, want) {
		t.Fatalf("frame=%x, want %x", got, want)
	}
}

func TestX365UDPConnSerializesConcurrentWrites(t *testing.T) {
	stream := newBlockingHeaderStream()
	conn := &x365UDPConn{stream: stream}

	firstDone := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("abc"))
		firstDone <- err
	}()
	receiveWithin(t, stream.firstHeaderWritten, "first UDP frame header")

	secondDone := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("de"))
		secondDone <- err
	}()

	close(stream.releaseFirstHeader)
	if err := receiveWithin(t, firstDone, "first UDP write"); err != nil {
		t.Fatal(err)
	}
	if err := receiveWithin(t, secondDone, "second UDP write"); err != nil {
		t.Fatal(err)
	}
	requireNoReceive(t, stream.overlap, "overlapping UDP header write")

	want := []byte{0, 3, 'a', 'b', 'c', 0, 2, 'd', 'e'}
	if got := stream.bytes(); !bytes.Equal(got, want) {
		t.Fatalf("UDP frames=%x, want %x", got, want)
	}
}

func TestX365UDPConnRejectsOversizedWrite(t *testing.T) {
	stream := &memoryStream{}
	conn := &x365UDPConn{stream: stream}
	_, err := conn.Write(bytes.Repeat([]byte("x"), x365UDPMaxPayload+1))
	if err == nil || !strings.Contains(err.Error(), "payload too large") {
		t.Fatalf("expected payload too large error, got %v", err)
	}
}

func TestX365UDPConnCloseIsIdempotent(t *testing.T) {
	stream := &memoryStream{}
	conn := &x365UDPConn{stream: stream}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if stream.closeCount != 1 {
		t.Fatalf("stream close count=%d, want 1", stream.closeCount)
	}
}

func TestX365UDPConnReadsDatagramAfterResponse(t *testing.T) {
	stream := &memoryStream{
		read: *bytes.NewBufferString("X365\x00\x00\x03abc"),
	}
	conn := &x365UDPConn{stream: stream}
	buf := make([]byte, 8)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "abc" {
		t.Fatalf("payload=%q, want abc", got)
	}
}

func TestX365UDPConnRejectsBadResponse(t *testing.T) {
	stream := &memoryStream{
		read: *bytes.NewBufferString("NOPE\x01\x00\x03abc"),
	}
	conn := &x365UDPConn{stream: stream}
	_, err := conn.Read(make([]byte, 8))
	if err == nil || !strings.Contains(err.Error(), "bad X365 response") {
		t.Fatalf("expected bad X365 response error, got %v", err)
	}
}

func TestX365UDPConnDropsRemainderWhenReadBufferTooSmall(t *testing.T) {
	stream := &memoryStream{
		read: *bytes.NewBufferString("X365\x00\x00\x05abcde\x00\x01z"),
	}
	conn := &x365UDPConn{stream: stream}
	buf := make([]byte, 3)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "abc" {
		t.Fatalf("first payload=%q, want abc", got)
	}

	buf = make([]byte, 3)
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "z" {
		t.Fatalf("second payload=%q, want z", got)
	}
}

func TestReadX365UDPDatagramReturnsPayloadEOF(t *testing.T) {
	_, err := readX365UDPDatagram(bytes.NewReader([]byte{0, 4, 'a'}), make([]byte, 8))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v, want unexpected EOF", err)
	}
}

type blockingHeaderStream struct {
	mu                 sync.Mutex
	writes             [][]byte
	firstHeaderWritten chan struct{}
	releaseFirstHeader chan struct{}
	overlap            chan struct{}
	firstHeaderBlocked bool
}

func newBlockingHeaderStream() *blockingHeaderStream {
	return &blockingHeaderStream{
		firstHeaderWritten: make(chan struct{}),
		releaseFirstHeader: make(chan struct{}),
		overlap:            make(chan struct{}, 1),
	}
}

func (s *blockingHeaderStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (s *blockingHeaderStream) Write(p []byte) (int, error) {
	copied := append([]byte(nil), p...)
	blockFirstHeader := false

	s.mu.Lock()
	if len(p) == 2 && len(s.writes) == 0 {
		blockFirstHeader = true
		s.firstHeaderBlocked = true
	} else if len(p) == 2 && s.firstHeaderBlocked {
		select {
		case s.overlap <- struct{}{}:
		default:
		}
	}
	s.writes = append(s.writes, copied)
	s.mu.Unlock()

	if blockFirstHeader {
		close(s.firstHeaderWritten)
		<-s.releaseFirstHeader
		s.mu.Lock()
		s.firstHeaderBlocked = false
		s.mu.Unlock()
	}
	return len(p), nil
}

func (s *blockingHeaderStream) Close() error {
	return nil
}

func (s *blockingHeaderStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []byte
	for _, write := range s.writes {
		out = append(out, write...)
	}
	return out
}
