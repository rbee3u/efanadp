package x365

import (
	"errors"
	"io"
	"testing"
	"time"
)

const testWaitTimeout = time.Second

type frame struct {
	frameHeader
	Payload []byte
}

func parseFrame(buf []byte) (frame, int, error) {
	if len(buf) < frameHeaderSize {
		return frame{}, 0, errors.New("need more data")
	}
	header := parseFrameHeader(buf[:frameHeaderSize])
	total := frameHeaderSize + header.PayloadLen
	if len(buf) < total {
		return frame{}, 0, errors.New("need more data")
	}
	return frame{
		frameHeader: header,
		Payload:     append([]byte(nil), buf[frameHeaderSize:total]...),
	}, total, nil
}

func readFrameForTest(r io.Reader) (frame, error) {
	header, err := readFrameHeader(r)
	if err != nil {
		return frame{}, err
	}
	result := frame{frameHeader: header}
	if header.PayloadLen > 0 {
		payload := make([]byte, header.PayloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return frame{}, err
		}
		result.Payload = payload
	}
	return result, nil
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
