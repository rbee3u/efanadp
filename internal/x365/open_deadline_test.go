package x365

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// A server that finishes TCP and TLS but never answers the X365 request used to
// block the caller forever: net/http2 has no response-header timeout, so the
// read in readX365OKResponse had no deadline at all.
func TestHandshakeWithDeadlineClosesStalledStream(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	handshake := func() (io.ReadWriteCloser, error) {
		// Stands in for readX365OKResponse against a silent server: it returns
		// only once the deadline closes the stream underneath it.
		return nil, stream.readUntilClosed()
	}

	conn, err := handshakeWithDeadline(t.Context(), 20*time.Millisecond, stream, handshake)
	if conn != nil {
		t.Fatal("stalled handshake returned a connection")
	}
	if !errors.Is(err, errOpenTimeout) {
		t.Fatalf("err=%v, want errOpenTimeout", err)
	}
	if stream.closeCount.Load() != 1 {
		t.Fatalf("stream close count=%d, want 1", stream.closeCount.Load())
	}
}

func TestHandshakeWithDeadlineKeepsSuccessfulStreamOpen(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	conn, err := handshakeWithDeadline(t.Context(), time.Minute, stream, func() (io.ReadWriteCloser, error) {
		return stream, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn != stream {
		t.Fatalf("conn=%v, want the handshake result", conn)
	}
	// The deadline must not outlive a handshake that succeeded.
	time.Sleep(20 * time.Millisecond)
	if got := stream.closeCount.Load(); got != 0 {
		t.Fatalf("stream close count=%d, want 0 after a successful handshake", got)
	}
}

// A cancelled parent must report cancellation, not a spurious timeout.
func TestHandshakeWithDeadlineReportsParentCancellation(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	ctx, cancel := context.WithCancel(t.Context())
	handshakeErr := errors.New("request cancelled")
	go func() {
		stream.waitForRead()
		cancel()
	}()

	_, err := handshakeWithDeadline(ctx, time.Minute, stream, func() (io.ReadWriteCloser, error) {
		_ = stream.readUntilClosed()
		return nil, handshakeErr
	})
	if !errors.Is(err, handshakeErr) {
		t.Fatalf("err=%v, want the handshake error", err)
	}
	if errors.Is(err, errOpenTimeout) {
		t.Fatal("parent cancellation was reported as a timeout")
	}
}

// When the deadline lands in the gap between a successful handshake and the
// closer being stopped, the returned conn wraps a stream that is already being
// closed. Handing it back would defer the timeout into an opaque relay error on
// the first Read, so the open must fail as a timeout instead.
func TestHandshakeWithDeadlineRejectsSuccessAfterDeadline(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	conn, err := handshakeWithDeadline(t.Context(), time.Millisecond, stream, func() (io.ReadWriteCloser, error) {
		// Return success only after the deadline has certainly fired.
		<-stream.closed
		return stream, nil
	})
	if conn != nil {
		t.Fatal("handshake that finished after the deadline returned a connection")
	}
	if !errors.Is(err, errOpenTimeout) {
		t.Fatalf("err=%v, want errOpenTimeout", err)
	}
}

func TestHandshakeWithDeadlineSkipsGuardWithoutTimeout(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	conn, err := handshakeWithDeadline(t.Context(), 0, stream, func() (io.ReadWriteCloser, error) {
		return stream, nil
	})
	if err != nil || conn != stream {
		t.Fatalf("conn=%v err=%v, want the handshake result", conn, err)
	}
}

// blockingCloseNotifyStream blocks in Read until Close, standing in for a stream
// whose peer never answers.
type blockingCloseNotifyStream struct {
	closed     chan struct{}
	reading    chan struct{}
	readOnce   atomic.Bool
	closeOnce  atomic.Bool
	closeCount atomic.Int64
}

func newBlockingCloseNotifyStream() *blockingCloseNotifyStream {
	return &blockingCloseNotifyStream{
		closed:  make(chan struct{}),
		reading: make(chan struct{}),
	}
}

func (s *blockingCloseNotifyStream) readUntilClosed() error {
	if s.readOnce.CompareAndSwap(false, true) {
		close(s.reading)
	}
	<-s.closed
	return io.ErrClosedPipe
}

func (s *blockingCloseNotifyStream) waitForRead() {
	<-s.reading
}

func (s *blockingCloseNotifyStream) Read(p []byte) (int, error) {
	if err := s.readUntilClosed(); err != nil {
		return 0, err
	}
	return 0, io.EOF
}

func (s *blockingCloseNotifyStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *blockingCloseNotifyStream) Close() error {
	s.closeCount.Add(1)
	if s.closeOnce.CompareAndSwap(false, true) {
		close(s.closed)
	}
	return nil
}
