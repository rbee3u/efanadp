package x365

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"
)

const testWaitTimeout = time.Second

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

type closeCountingTestStream interface {
	io.ReadWriteCloser
	closes() int
}

type memoryStream struct {
	read       bytes.Buffer
	write      bytes.Buffer
	closeCount int
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
