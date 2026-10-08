package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestIsBenignClientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "eof", err: io.EOF, want: true},
		{name: "closed pipe", err: io.ErrClosedPipe, want: true},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, want: true},
		{name: "net closed", err: net.ErrClosed, want: true},
		// The SOCKS handshake runs under a deadline, so a client that connects and
		// then stalls times out on every attempt.
		{name: "net timeout", err: os.ErrDeadlineExceeded, want: true},
		{name: "closed text without standard error", err: errors.New("upstream closed"), want: false},
		{name: "upstream failure is not benign", err: errors.New("bad X365 response"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBenignClientError(fmt.Errorf("relay failed: %w", tt.err)); got != tt.want {
				t.Fatalf("isBenignClientError(%v)=%v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestRelayTCPCancelClosesBothSides(t *testing.T) {
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
		done <- relayTCP(ctx, client, upstream, &Stats{}, nil)
	}()

	cancel()
	if err := receiveWithin(t, done, "relay shutdown"); err != nil {
		t.Fatalf("relayTCP returned %v", err)
	}

	assertPipeWriteClosed(t, clientPeer)
	assertPipeWriteClosed(t, upstreamPeer)
}

func TestRunRelayPairWaitsForBothDirectionsAndPreservesFailures(t *testing.T) {
	upstreamErr := errors.New("upstream failure")
	tests := []struct {
		name   string
		first  error
		second error
		want   error
	}{
		{name: "both succeed"},
		{name: "both disconnect", first: io.EOF, second: io.ErrClosedPipe},
		{name: "failure before disconnect", first: upstreamErr, second: io.EOF, want: upstreamErr},
		{name: "failure after disconnect", first: io.EOF, second: upstreamErr, want: upstreamErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				done := make(chan error, 1)
				var stopCount atomic.Int32
				go func() {
					done <- runRelayPair(func() { stopCount.Add(1) },
						func() error { return tt.first },
						func() error { <-release; return tt.second })
				}()
				synctest.Wait()
				if got := stopCount.Load(); got != 1 {
					t.Errorf("stop count=%d after first direction finished, want 1", got)
				}
				select {
				case err := <-done:
					t.Errorf("relay returned %v before second direction finished", err)
				default:
				}
				close(release)
				synctest.Wait()
				select {
				case err := <-done:
					if !errors.Is(err, tt.want) {
						t.Errorf("relay error=%v, want %v", err, tt.want)
					}
				default:
					t.Error("relay did not return after both directions finished")
				}
				if got := stopCount.Load(); got != 1 {
					t.Errorf("stop count=%d, want exactly 1", got)
				}
			})
		})
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
			relayWriter{writer: &dst, add: stats.AddUpload},
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
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("set pipe write deadline: %v", err)
	}
	if _, err := conn.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after relay shutdown=%v, want closed pipe", err)
	}
}
