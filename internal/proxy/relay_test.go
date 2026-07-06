package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnexpectedEOFIsBenignClientError(t *testing.T) {
	err := fmt.Errorf("relay failed: %w", io.ErrUnexpectedEOF)
	if !isBenignClientError(err) {
		t.Fatalf("expected unexpected EOF to be benign")
	}
}

func TestHTTP2ResponseBodyClosedIsBenignClientError(t *testing.T) {
	err := fmt.Errorf("relay failed: %w", errors.New("http2: response body closed"))
	if !isBenignClientError(err) {
		t.Fatalf("expected HTTP/2 response body close to be benign")
	}
}

func TestRelayMultipathCancelClosesBothSides(t *testing.T) {
	clientPeer, client := net.Pipe()
	defer func() {
		_ = clientPeer.Close()
	}()
	upstreamPeer, upstream := net.Pipe()
	defer func() {
		_ = upstreamPeer.Close()
	}()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- relayMultipath(ctx, client, upstream, &Stats{}, nil)
	}()

	cancel()
	if err := receiveWithin(t, done, "relay shutdown"); err != nil {
		t.Fatalf("relayMultipath returned %v", err)
	}

	assertPipeWriteClosed(t, clientPeer)
	assertPipeWriteClosed(t, upstreamPeer)
}

func TestRunRelayPairStopsOnceAndReturnsNonBenignError(t *testing.T) {
	stopCh := make(chan struct{})
	var stopCount atomic.Int32
	errBoom := errors.New("boom")

	err := runRelayPair(func() {
		if stopCount.Add(1) == 1 {
			close(stopCh)
		}
	}, func() error {
		return errBoom
	}, func() error {
		<-stopCh
		return io.EOF
	})

	if got := stopCount.Load(); got != 1 {
		t.Fatalf("stop count=%d, want 1", got)
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("relay error=%v, want boom", err)
	}
}

func TestRunRelayPairStopsOnceWhenBothTasksReturn(t *testing.T) {
	var stopCount atomic.Int32

	err := runRelayPair(func() {
		stopCount.Add(1)
	}, func() error {
		return nil
	}, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("relay error=%v, want nil", err)
	}
	if got := stopCount.Load(); got != 1 {
		t.Fatalf("stop count=%d, want 1", got)
	}
}

func TestRunRelayPairReturnsNilForOnlyBenignErrors(t *testing.T) {
	err := runRelayPair(nil, func() error {
		return io.EOF
	}, func() error {
		return io.ErrClosedPipe
	})
	if err != nil {
		t.Fatalf("relay error=%v, want nil", err)
	}
}

func BenchmarkRelayCopy(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 256*1024)
	b.ReportAllocs()
	for b.Loop() {
		var dst bytes.Buffer
		stats := &Stats{}
		err := copyWithPooledBuffer(
			countingWriter{writer: &dst, add: stats.AddUpload},
			bytes.NewReader(payload),
		)
		if err != nil {
			b.Fatal(err)
		}
		if dst.Len() != len(payload) || stats.Upload() != int64(len(payload)) {
			b.Fatalf("copied=%d stats=%d want %d", dst.Len(), stats.Upload(), len(payload))
		}
	}
}

func assertPipeWriteClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("write succeeded after relay shutdown")
	}
}
