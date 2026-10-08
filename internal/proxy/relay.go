package proxy

import (
	"context"
	"io"
	"net"
	"sync"
)

// runRelayPair waits for both directions. stop runs once after the first
// finishes and must unblock the other. The first non-benign error is returned.
func runRelayPair(stop func(), left, right func() error) error {
	if stop != nil {
		stop = sync.OnceFunc(stop)
	}

	errCh := make(chan error, 2)
	run := func(task func() error) {
		err := task()
		if stop != nil {
			stop()
		}
		errCh <- err
	}

	go run(left)
	go run(right)

	var firstErr error
	for range 2 {
		err := <-errCh
		if firstErr == nil && err != nil && !isBenignClientError(err) {
			firstErr = err
		}
	}
	return firstErr
}

const relayBufferSize = 32 * 1024

var relayBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, relayBufferSize)
		return &buf
	},
}

func copyWithPooledBuffer(dst io.Writer, src io.Reader) error {
	bufPtr, ok := relayBufferPool.Get().(*[]byte)
	if !ok {
		buf := make([]byte, relayBufferSize)
		bufPtr = &buf
	}
	buf := *bufPtr
	defer relayBufferPool.Put(bufPtr)
	_, err := io.CopyBuffer(dst, src, buf)
	return err
}

func relayTCP(
	ctx context.Context,
	client net.Conn,
	upstream io.ReadWriteCloser,
	liveStats *Stats,
	observeUpload func([]byte),
) error {
	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	closeBoth := sync.OnceFunc(func() {
		closeQuietly(upstream)
		closeQuietly(client)
	})
	stopClosing := context.AfterFunc(relayCtx, closeBoth)
	defer func() {
		stopClosing()
		closeBoth()
	}()

	stopRelay := func() {
		cancel()
		closeBoth()
	}
	upload := func() error {
		writer := relayWriter{
			writer:  upstream,
			observe: observeUpload,
			add:     liveStats.AddUpload,
		}
		return copyWithPooledBuffer(writer, client)
	}
	download := func() error {
		return copyWithPooledBuffer(relayWriter{writer: client, add: liveStats.AddDownload}, upstream)
	}

	return runRelayPair(stopRelay, upload, download)
}

type relayWriter struct {
	writer  io.Writer
	observe func([]byte)
	add     func(int64)
}

func (w relayWriter) Write(p []byte) (int, error) {
	if w.observe != nil && len(p) > 0 {
		w.observe(p)
	}
	n, err := w.writer.Write(p)
	if n > 0 && w.add != nil {
		w.add(int64(n))
	}
	return n, err
}
