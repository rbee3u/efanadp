package proxy

import (
	"bufio"
	"fmt"
	"io"
	"testing"
	"time"
)

const testWaitTimeout = time.Second

func socksReadConnect(reader *bufio.Reader, writer io.Writer) (string, int, error) {
	request, err := socksReadRequest(reader, writer)
	if err != nil {
		return "", 0, err
	}
	if request.Command != socksCommandConnect {
		writeSOCKSReplyQuietly(writer, 7)
		return "", 0, fmt.Errorf("unsupported SOCKS5 command: %s", socksCommandName(request.Command))
	}
	return request.Host, request.Port, nil
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

func requireNoReceive[T any](tb testing.TB, ch <-chan T, label string) {
	tb.Helper()
	select {
	case value := <-ch:
		tb.Fatalf("received %s: %v", label, value)
	default:
	}
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
