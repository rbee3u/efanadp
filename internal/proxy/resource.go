package proxy

import (
	"context"
	"io"
	"net"
	"time"
)

func closeWhenContextDone(ctx context.Context, closer io.Closer) func() bool {
	return context.AfterFunc(ctx, func() {
		closeQuietly(closer)
	})
}

func closeQuietly(closer io.Closer) {
	if closer == nil {
		return
	}
	_ = closer.Close()
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

func setDeadlineQuietly(conn net.Conn, deadline time.Time) {
	if conn == nil {
		return
	}
	_ = conn.SetDeadline(deadline)
}
