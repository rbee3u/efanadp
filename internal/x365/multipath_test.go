package x365

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// Opening a multipath stream is a sequence of writes and reads, and a failure at
// any step has to close the stream and name the step. The stream owns a TCP
// connection and its HTTP/2 state, so one left open pins both for the life of the
// process; the step name is what tells the user where a node stopped working.
//
// Two steps are unreachable from here: the request build takes fixed control-host
// constants, and the framer comes from crypto/rand.
func TestOpenMultipathFailuresCloseStreamAndNameStage(t *testing.T) {
	errBoom := errors.New("write failed")

	tests := []struct {
		name string
		// newStream supplies the upstream this open runs against.
		newStream func() closeCountingTestStream
		destPort  int
		want      string
		wantIs    error
	}{
		{
			name:      "write request",
			newStream: func() closeCountingTestStream { return &writeErrorStream{err: errBoom} },
			destPort:  443,
			want:      "write x365 multipath request",
			wantIs:    errBoom,
		},
		{
			name: "read response",
			newStream: func() closeCountingTestStream {
				return &memoryStream{read: *bytes.NewBufferString("NOPE\x01")}
			},
			destPort: 443,
			want:     "bad X365 response",
		},
		{
			name: "build hello",
			newStream: func() closeCountingTestStream {
				return &memoryStream{read: *bytes.NewBufferString("X365\x00")}
			},
			// An unencodable port fails after the response is accepted.
			destPort: -1,
			want:     "build multipath hello",
		},
		{
			name: "write hello",
			newStream: func() closeCountingTestStream {
				// The request write succeeds and the response is accepted, so the
				// second write is the hello.
				return &writeFailAtStream{
					failOn: 2,
					err:    errBoom,
					read:   *bytes.NewBufferString("X365\x00"),
				}
			},
			destPort: 443,
			want:     "write multipath hello",
			wantIs:   errBoom,
		},
	}

	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := tt.newStream()

			conn, err := openMultipath(stream, userID, "example.com", tt.destPort)

			if err == nil {
				closeQuietly(conn)
				t.Fatal("openMultipath succeeded, want a failure")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want context %q", err, tt.want)
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

// The server answers only after it has read the request, so the request has to go
// out before the response is validated.
func TestOpenMultipathWritesRequestBeforeReadingResponse(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBufferString("NOPE\x01")}

	if _, err := openMultipath(stream, userID, "example.com", 443); err == nil {
		t.Fatal("openMultipath succeeded, want a bad-response failure")
	}
	if stream.write.Len() == 0 {
		t.Fatal("no X365 request was written before response validation")
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
	conn := newMultipathConn(stream, &multipathFramer{})
	defer func() { _ = conn.Close() }()

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
	conn := newMultipathConn(stream, &multipathFramer{})
	defer func() { _ = conn.Close() }()

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

func TestMultipathConnReadLargeFrameBuffersOnlyRemainder(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 128*1024)
	frameBytes, err := buildFrame(frameData, 7, payload)
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBuffer(frameBytes)}
	conn := newMultipathConn(stream, &multipathFramer{})
	defer func() { _ = conn.Close() }()

	buf := make([]byte, 64*1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	prefixMatch := bytes.Equal(buf, payload[:len(buf)])
	if n != len(buf) || !prefixMatch {
		t.Fatalf("read n=%d prefix match=%t, want first %d bytes", n, prefixMatch, len(buf))
	}
	if !bytes.Equal(conn.readBuf, payload[len(buf):]) {
		t.Fatalf("buffered remainder len=%d, want %d", len(conn.readBuf), len(payload)-len(buf))
	}
	assertSingleACKFrame(t, stream.write.Bytes(), 7+uint64(len(payload)))
}

func TestMultipathConnReadLargeFrameEOFInRemainderDoesNotACK(t *testing.T) {
	frameBytes, err := buildFrame(frameData, 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	stream := &memoryStream{read: *bytes.NewBuffer(frameBytes[:frameHeaderSize+3])}
	conn := newMultipathConn(stream, &multipathFramer{})
	defer func() { _ = conn.Close() }()

	buf := make([]byte, 2)
	n, err := conn.Read(buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err=%v, want unexpected EOF", err)
	}
	if n != 0 {
		t.Fatalf("read n=%d, want 0 for incomplete frame", n)
	}
	if len(conn.readBuf) != 0 {
		t.Fatalf("readBuf len=%d, want no buffered partial frame", len(conn.readBuf))
	}
	if stream.write.Len() != 0 {
		t.Fatalf("ACK bytes=%d, want no ACK for incomplete frame", stream.write.Len())
	}
}

func TestMultipathConnCloseIsIdempotent(t *testing.T) {
	stream := &memoryStream{}
	conn := newMultipathConn(stream, &multipathFramer{})

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
	conn := newMultipathConn(stream, &multipathFramer{})

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

func TestMultipathConnCloseBoundsStalledCloseFrame(t *testing.T) {
	stream := newBlockingWriteStream()
	conn := newMultipathConn(stream, &multipathFramer{})

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- conn.Close()
	}()
	receiveWithin(t, stream.writeStarted, "stalled multipath close frame")
	if err := receiveWithin(t, closeDone, "multipath close after stalled close frame"); err != nil {
		t.Fatal(err)
	}
}

// A payload write can fail after its frame header is already on the wire. The
// stream is unrecoverable at that point: advancing the sequence, retrying a new
// frame, or appending a close/ACK frame would make the peer interpret control
// bytes as the missing payload.
func TestMultipathConnWriteFailureIsTerminal(t *testing.T) {
	errBoom := errors.New("payload write failed")
	stream := &failNthRecordingStream{failOn: 2, err: errBoom}
	framer := &multipathFramer{WriteSeq: 7}
	conn := newMultipathConn(stream, framer)

	if _, err := conn.Write([]byte("abc")); !errors.Is(err, errBoom) {
		t.Fatalf("first write err=%v, want %v", err, errBoom)
	}
	if got := framer.WriteSeq; got != 7 {
		t.Fatalf("write sequence=%d after failed frame, want 7", got)
	}
	if _, err := conn.Write([]byte("retry")); !errors.Is(err, errBoom) {
		t.Fatalf("retry err=%v, want original terminal error", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	written := stream.bytes()
	if len(written) != frameHeaderSize {
		t.Fatalf("written bytes=%d, want only the failed frame header", len(written))
	}
	header, payloadLen := parseFrameHeader(written)
	if header.Type != frameData || header.Seq != 7 || payloadLen != 3 {
		t.Fatalf("failed frame header=%+v payloadLen=%d", header, payloadLen)
	}
	if got := stream.writeCalls(); got != 2 {
		t.Fatalf("underlying write calls=%d, want header and failed payload only", got)
	}
}

// TestMultipathConnReadDoesNotWaitForStalledWrite covers the deadlock the ACK
// queue exists to prevent: a data frame write parked in the upstream
// flow-control window must not stop the read side from delivering downloads.
func TestMultipathConnReadDoesNotWaitForStalledWrite(t *testing.T) {
	frameBytes, err := buildFrame(frameData, 7, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	stream := newStalledWriteStream(frameBytes)
	conn := newMultipathConn(stream, &multipathFramer{})

	writeDone := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("upload payload"))
		writeDone <- err
	}()
	receiveWithin(t, stream.writeStarted, "stalled multipath write")

	readDone := make(chan string, 1)
	go func() {
		buf := make([]byte, 5)
		n, readErr := conn.Read(buf)
		if readErr != nil {
			readDone <- "error: " + readErr.Error()
			return
		}
		readDone <- string(buf[:n])
	}()
	if got := receiveWithin(t, readDone, "read while the write path is stalled"); got != "hello" {
		t.Fatalf("read %q, want hello", got)
	}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receiveWithin(t, writeDone, "stalled write release"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write err=%v, want closed pipe", err)
	}
}

// TestMultipathConnQueuedACKIsWrittenAfterWriteUnblocks shows a contended ACK is
// deferred rather than dropped: once the busy frame write lock frees, the sender
// goroutine writes the coalesced sequence, and only that newest one.
func TestMultipathConnQueuedACKIsWrittenAfterWriteUnblocks(t *testing.T) {
	stream := newRecordingStream()
	conn := newMultipathConn(stream, &multipathFramer{})
	defer func() { _ = conn.Close() }()

	// Holding the lock is what a write stalled in the flow-control window does.
	conn.writeMu.Lock()
	conn.ackDataFrame(11)
	conn.ackDataFrame(12)
	if written := stream.written(); len(written) != 0 {
		t.Fatalf("wrote %d bytes while the write lock was held, want none", len(written))
	}
	conn.writeMu.Unlock()

	assertSingleACKFrame(t, receiveWithin(t, stream.writes, "queued ACK frame"), 12)
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
	conn := newMultipathConn(discardStream{}, &multipathFramer{})
	defer func() { _ = conn.Close() }()

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

func BenchmarkMultipathConnReadLargeFrameSmallBuffer(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 128*1024)
	frameBytes, err := buildFrame(frameData, 99, payload)
	if err != nil {
		b.Fatal(err)
	}
	buf := make([]byte, 64*1024)
	reader := bytes.NewReader(frameBytes)
	stream := &readDiscardStream{reader: reader}

	b.ReportAllocs()
	for b.Loop() {
		reader.Reset(frameBytes)
		conn := newMultipathConn(stream, &multipathFramer{})
		n, err := conn.Read(buf)
		if err != nil {
			b.Fatal(err)
		}
		if n != len(buf) {
			b.Fatalf("read %d bytes, want %d", n, len(buf))
		}
		if len(conn.readBuf) != len(payload)-len(buf) {
			b.Fatalf("buffered %d bytes, want %d", len(conn.readBuf), len(payload)-len(buf))
		}
		if err := conn.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// closeCountingTestStream is an upstream fixture that reports how many times it
// was closed, so a test can assert an open closed it exactly once.
type closeCountingTestStream interface {
	io.ReadWriteCloser
	closes() int
}

type memoryStream struct {
	read       bytes.Buffer
	write      bytes.Buffer
	closeCount int
}

type discardStream struct{}

type readDiscardStream struct {
	reader *bytes.Reader
}

func (discardStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (discardStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (discardStream) Close() error {
	return nil
}

func (s *readDiscardStream) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *readDiscardStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *readDiscardStream) Close() error {
	return nil
}

func (s *memoryStream) Read(p []byte) (int, error) {
	return s.read.Read(p)
}

func (s *memoryStream) Write(p []byte) (int, error) {
	return s.write.Write(p)
}

func (s *memoryStream) Close() error {
	s.closeCount++
	return nil
}

func (s *memoryStream) closes() int {
	return s.closeCount
}

// writeFailAtStream accepts writes until the failOn-th one, which stands in for an
// upstream that drops the connection partway through the open sequence.
type writeFailAtStream struct {
	failOn     int
	err        error
	read       bytes.Buffer
	writes     int
	closeCount int
}

type failNthRecordingStream struct {
	mu        sync.Mutex
	written   bytes.Buffer
	failOn    int
	err       error
	writes    int
	closeOnce sync.Once
}

func (s *failNthRecordingStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (s *failNthRecordingStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.writes == s.failOn {
		return 0, s.err
	}
	return s.written.Write(p)
}

func (s *failNthRecordingStream) Close() error {
	s.closeOnce.Do(func() {})
	return nil
}

func (s *failNthRecordingStream) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.written.Bytes())
}

func (s *failNthRecordingStream) writeCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

func (s *writeFailAtStream) Read(p []byte) (int, error) {
	return s.read.Read(p)
}

func (s *writeFailAtStream) Write(p []byte) (int, error) {
	s.writes++
	if s.writes >= s.failOn {
		return 0, s.err
	}
	return len(p), nil
}

func (s *writeFailAtStream) Close() error {
	s.closeCount++
	return nil
}

func (s *writeFailAtStream) closes() int {
	return s.closeCount
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

func (s *writeErrorStream) closes() int {
	return s.closeCount
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

// stalledWriteStream stands in for an upstream whose flow-control window is
// full: writes park until Close, while reads keep delivering frames.
type stalledWriteStream struct {
	writeStarted chan struct{}
	closed       chan struct{}
	writeOnce    sync.Once
	closeOnce    sync.Once

	mu   sync.Mutex
	read *bytes.Reader
}

func newStalledWriteStream(readable []byte) *stalledWriteStream {
	return &stalledWriteStream{
		writeStarted: make(chan struct{}),
		closed:       make(chan struct{}),
		read:         bytes.NewReader(readable),
	}
}

func (s *stalledWriteStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read.Read(p)
}

func (s *stalledWriteStream) Write([]byte) (int, error) {
	s.writeOnce.Do(func() {
		close(s.writeStarted)
	})
	<-s.closed
	return 0, io.ErrClosedPipe
}

func (s *stalledWriteStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
	})
	return nil
}

// recordingStream reports each write on a channel so a test can wait for one
// that another goroutine produces.
type recordingStream struct {
	writes chan []byte

	mu    sync.Mutex
	bytes []byte
}

func newRecordingStream() *recordingStream {
	return &recordingStream{writes: make(chan []byte, 8)}
}

func (s *recordingStream) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (s *recordingStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.bytes = append(s.bytes, p...)
	s.mu.Unlock()

	select {
	case s.writes <- append([]byte(nil), p...):
	default:
	}
	return len(p), nil
}

func (s *recordingStream) Close() error {
	return nil
}

func (s *recordingStream) written() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.bytes...)
}
