package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const testWaitTimeout = time.Second

func readSOCKSConnect(reader *bufio.Reader, writer io.Writer) (string, int, error) {
	request, err := readSOCKSRequest(reader, writer)
	if err != nil {
		return "", 0, err
	}
	if request.Command != socksCommandConnect {
		writeSOCKSReplyQuietly(writer, socksReplyCommandNotSupported)
		return "", 0, fmt.Errorf("unsupported SOCKS5 command: %s", socksCommandName(request.Command))
	}
	return request.Host, request.Port, nil
}

// Inspect and refresh cached targets without initiating an open.
func (a *udpAssociation) lookupTarget(key string) (*udpTarget, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.isClosed {
		return nil, net.ErrClosed
	}
	target := a.targets[key]
	if target != nil {
		target.touch(time.Now())
	}
	return target, nil
}

func receiveWithin[T any](tb testing.TB, ch <-chan T, label string) T {
	tb.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(testWaitTimeout):
		tb.Fatalf("timed out waiting for %s", label)
	}
	var zero T
	return zero
}

func waitFor(tb testing.TB, cond func() bool, label string) {
	tb.Helper()
	deadline := time.Now().Add(testWaitTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			tb.Fatalf("timed out waiting for %s", label)
		}
		time.Sleep(time.Millisecond)
	}
}

func requireNoReceive[T any](tb testing.TB, ch <-chan T, label string) {
	tb.Helper()
	select {
	case value := <-ch:
		tb.Fatalf("received %s: %v", label, value)
	default:
	}
}

// Target opens and relays log concurrently with test actions.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *logRecorder) logger() testLogger {
	return testLogger{log: func(_ string, format string, args ...any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.lines = append(r.lines, fmt.Sprintf(format, args...))
	}}
}

func (r *logRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

func (r *logRecorder) count(substr string) int {
	n := 0
	for _, line := range r.all() {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

func receiveMatchingWithin[T any](tb testing.TB, ch <-chan T, label string, matches func(T) bool) T {
	tb.Helper()
	timer := time.NewTimer(testWaitTimeout)
	defer timer.Stop()
	for {
		select {
		case value := <-ch:
			if matches(value) {
				return value
			}
		case <-timer.C:
			tb.Fatalf("timed out waiting for %s", label)
		}
	}
}
