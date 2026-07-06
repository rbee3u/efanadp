package x365

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestOpenMultipathBadResponseClosesStream(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBufferString("NOPE\x01")}
	_, err = openMultipath(stream, userID, "example.com", 443)
	if err == nil || !strings.Contains(err.Error(), "bad X365 response") {
		t.Fatalf("expected bad response error, got %v", err)
	}
	if !stream.closed {
		t.Fatal("expected stream to be closed")
	}
	if stream.write.Len() == 0 {
		t.Fatal("expected X365 request to be written before response validation")
	}
}

func TestOpenMultipathWriteErrorWrapsAndClosesStream(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	errBoom := errors.New("write failed")
	stream := &writeErrorStream{err: errBoom}
	_, err = openMultipath(stream, userID, "example.com", 443)
	if !errors.Is(err, errBoom) {
		t.Fatalf("err=%v, want write failure in chain", err)
	}
	if !strings.Contains(err.Error(), "write x365 multipath request") {
		t.Fatalf("err=%v, want multipath write context", err)
	}
	if stream.closeCount != 1 {
		t.Fatalf("stream close count=%d, want 1", stream.closeCount)
	}
}

func TestOpenMultipathHelloErrorClosesStream(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBufferString("X365\x00")}
	_, err = openMultipath(stream, userID, "example.com", -1)
	if err == nil || !strings.Contains(err.Error(), "build multipath hello") {
		t.Fatalf("err=%v, want multipath hello context", err)
	}
	if stream.closeCount != 1 {
		t.Fatalf("stream close count=%d, want 1", stream.closeCount)
	}
}

func TestOpenX365MultipathWrapsTransportError(t *testing.T) {
	_, err := openX365Multipath(t.Context(), Node{}, UserID{}, 0, "example.com", 443)
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

func TestReadFrameParsesDataACKAndClose(t *testing.T) {
	dataFrame, err := buildFrame(frameData, 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	ackFrame, err := buildFrame(frameACK, 12, nil)
	if err != nil {
		t.Fatal(err)
	}
	closeFrame, err := buildFrame(frameClose, 12, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream := bytes.NewBuffer(append(append(dataFrame, ackFrame...), closeFrame...))

	got, err := readFrameForTest(stream)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != frameData || got.Seq != 7 || string(got.Payload) != "hello" {
		t.Fatalf("data frame=%+v", got)
	}
	got, err = readFrameForTest(stream)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != frameACK || got.Seq != 12 || len(got.Payload) != 0 {
		t.Fatalf("ack frame=%+v", got)
	}
	got, err = readFrameForTest(stream)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != frameClose || got.Seq != 12 || len(got.Payload) != 0 {
		t.Fatalf("close frame=%+v", got)
	}
}

func TestReadFrameReturnsPayloadEOF(t *testing.T) {
	frameBytes, err := buildFrame(frameData, 1, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readFrameForTest(bytes.NewReader(frameBytes[:len(frameBytes)-2]))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v, want unexpected EOF", err)
	}
}

func TestMultipathConnReadDirectDataFrameAndACKs(t *testing.T) {
	frameBytes, err := buildFrame(frameData, 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBuffer(frameBytes)}
	conn := &multipathConn{stream: stream, framer: &multipathFramer{}}

	buf := make([]byte, 5)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("read n=%d buf=%q, want hello", n, buf)
	}
	assertSingleACKFrame(t, stream.write.Bytes(), 12)
	if len(conn.readBuf) != 0 {
		t.Fatalf("readBuf len=%d, want direct read without buffering", len(conn.readBuf))
	}
}

func TestMultipathConnReadBuffersPayloadRemainderAndACKsOnce(t *testing.T) {
	frameBytes, err := buildFrame(frameData, 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBuffer(frameBytes)}
	conn := &multipathConn{stream: stream, framer: &multipathFramer{}}

	buf := make([]byte, 2)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || string(buf) != "he" {
		t.Fatalf("first read n=%d buf=%q, want he", n, buf)
	}

	buf = make([]byte, 3)
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || string(buf) != "llo" {
		t.Fatalf("second read n=%d buf=%q, want llo", n, buf)
	}
	assertSingleACKFrame(t, stream.write.Bytes(), 12)
}

func TestMultipathConnCloseIsIdempotent(t *testing.T) {
	stream := &memoryStream{}
	conn := &multipathConn{stream: stream, framer: &multipathFramer{}}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if stream.closeCount != 1 {
		t.Fatalf("stream close count=%d, want 1", stream.closeCount)
	}

	got, n, err := parseFrame(stream.write.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != frameClose {
		t.Fatalf("frame type=%d, want close", got.Type)
	}
	if n != stream.write.Len() {
		t.Fatalf("close frame bytes=%d, total bytes=%d; close frame was written more than once", n, stream.write.Len())
	}
}

func TestMultipathConnCloseUnblocksActiveWrite(t *testing.T) {
	stream := newBlockingWriteStream()
	conn := &multipathConn{stream: stream, framer: &multipathFramer{}}

	writeDone := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("payload"))
		writeDone <- err
	}()
	receiveWithin(t, stream.writeStarted, "active multipath write")

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- conn.Close()
	}()
	if err := receiveWithin(t, closeDone, "multipath close with blocked writer"); err != nil {
		t.Fatal(err)
	}
	if err := receiveWithin(t, writeDone, "blocked multipath write release"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write err=%v, want closed pipe", err)
	}
}

func assertSingleACKFrame(t *testing.T, data []byte, wantSeq uint64) {
	t.Helper()

	got, n, err := parseFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != frameACK || got.Seq != wantSeq {
		t.Fatalf("ack frame type=%d seq=%d, want ACK seq=%d", got.Type, got.Seq, wantSeq)
	}
	if n != len(data) {
		t.Fatalf("ack frame bytes=%d, total bytes=%d; ACK was written more than once", n, len(data))
	}
}

func BenchmarkReadFrame(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 16*1024)
	frameBytes, err := buildFrame(frameData, 99, payload)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		reader := bytes.NewReader(frameBytes)
		got, err := readFrameForTest(reader)
		if err != nil {
			b.Fatal(err)
		}
		if len(got.Payload) != len(payload) {
			b.Fatalf("payload len=%d, want %d", len(got.Payload), len(payload))
		}
	}
}

func BenchmarkMultipathConnWrite(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 16*1024)
	conn := &multipathConn{
		stream: discardStream{},
		framer: &multipathFramer{},
	}

	b.ReportAllocs()
	for b.Loop() {
		n, err := conn.Write(payload)
		if err != nil {
			b.Fatal(err)
		}
		if n != len(payload) {
			b.Fatalf("wrote %d bytes, want %d", n, len(payload))
		}
	}
}

type memoryStream struct {
	read       bytes.Buffer
	write      bytes.Buffer
	closed     bool
	closeCount int
}

type discardStream struct{}

func (discardStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (discardStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discardStream) Close() error {
	return nil
}

func (s *memoryStream) Read(p []byte) (int, error) {
	return s.read.Read(p)
}

func (s *memoryStream) Write(p []byte) (int, error) {
	return s.write.Write(p)
}

func (s *memoryStream) Close() error {
	s.closed = true
	s.closeCount++
	return nil
}

type writeErrorStream struct {
	err        error
	closeCount int
}

func (s *writeErrorStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (s *writeErrorStream) Write([]byte) (int, error) {
	return 0, s.err
}

func (s *writeErrorStream) Close() error {
	s.closeCount++
	return nil
}

type blockingWriteStream struct {
	writeStarted chan struct{}
	closed       chan struct{}
	writeOnce    sync.Once
	closeOnce    sync.Once
}

func newBlockingWriteStream() *blockingWriteStream {
	return &blockingWriteStream{
		writeStarted: make(chan struct{}),
		closed:       make(chan struct{}),
	}
}

func (s *blockingWriteStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (s *blockingWriteStream) Write([]byte) (int, error) {
	s.writeOnce.Do(func() {
		close(s.writeStarted)
	})
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *blockingWriteStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}
