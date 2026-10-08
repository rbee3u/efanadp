package x365

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

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
	synctest.Test(t, func(t *testing.T) {
		const timeout = time.Minute

		stream := newBlockingCloseNotifyStream()
		conn, err := handshakeWithDeadline(t.Context(), timeout, stream, func() (io.ReadWriteCloser, error) {
			return stream, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if conn != stream {
			t.Fatalf("conn=%v, want the handshake result", conn)
		}

		time.Sleep(2 * timeout)
		synctest.Wait()

		if got := stream.closeCount.Load(); got != 0 {
			t.Fatalf("stream close count=%d, want 0 after a successful handshake", got)
		}
	})
}

func TestHandshakeWithDeadlineReportsParentCancellation(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		stream.waitForRead()
		cancel()
	}()

	_, err := handshakeWithDeadline(ctx, time.Minute, stream, func() (io.ReadWriteCloser, error) {
		return nil, stream.readUntilClosed()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if errors.Is(err, errOpenTimeout) {
		t.Fatal("parent cancellation was reported as a timeout")
	}
}

func TestHandshakeWithDeadlineRejectsSuccessAfterDeadline(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	conn, err := handshakeWithDeadline(t.Context(), time.Millisecond, stream, func() (io.ReadWriteCloser, error) {
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

func TestHandshakeWithDeadlineRejectsSuccessAfterParentCancellation(t *testing.T) {
	stream := newBlockingCloseNotifyStream()
	ctx, cancel := context.WithCancel(t.Context())
	conn, err := handshakeWithDeadline(ctx, time.Minute, stream, func() (io.ReadWriteCloser, error) {
		cancel()
		<-stream.closed
		return stream, nil
	})
	if conn != nil {
		t.Fatal("handshake that finished after parent cancellation returned a connection")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
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
