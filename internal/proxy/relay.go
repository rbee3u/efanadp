package proxy

import (
	"context"
	"io"
	"net"
	"sync"
)

type relayTask func() error

func runRelayPair(stop func(), left, right relayTask) error {
	if stop != nil {
		stop = sync.OnceFunc(stop)
	}

	errCh := make(chan error, 2)
	run := func(task relayTask) {
		err := task()
		if stop != nil {
			stop()
		}
		errCh <- err
	}

	var group sync.WaitGroup
	group.Go(func() { run(left) })
	group.Go(func() { run(right) })
	group.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil && !isBenignClientError(err) {
			return err
		}
	}
	return nil
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
	if !ok || bufPtr == nil || len(*bufPtr) != relayBufferSize {
		buf := make([]byte, relayBufferSize)
		bufPtr = &buf
	}
	buf := *bufPtr
	defer relayBufferPool.Put(bufPtr)
	_, err := io.CopyBuffer(dst, src, buf)
	return err
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

func relayMultipath(
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
	stopClosing := runWhenContextDone(relayCtx, closeBoth)
	defer func() {
		stopClosing()
		closeBoth()
	}()

	stopRelay := func() {
		cancel()
		closeBoth()
	}
	upload := func() error {
		writer := uploadObserver{
			writer:  upstream,
			observe: observeUpload,
		}
		return copyWithPooledBuffer(
			countingWriter{writer: writer, add: liveStats.AddUpload},
			client,
		)
	}
	download := func() error {
		return copyWithPooledBuffer(countingWriter{writer: client, add: liveStats.AddDownload}, upstream)
	}

	return runRelayPair(stopRelay, upload, download)
}

type uploadObserver struct {
	writer  io.Writer
	observe func([]byte)
}

func (w uploadObserver) Write(p []byte) (int, error) {
	if w.observe != nil && len(p) > 0 {
		w.observe(p)
	}
	return w.writer.Write(p)
}

type countingWriter struct {
	writer io.Writer
	add    func(int64)
}

func (w countingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if n > 0 && w.add != nil {
		w.add(int64(n))
	}
	return n, err
}
