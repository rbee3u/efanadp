package proxy

import (
	"context"
	"io"
	"net"
	"time"
)

func closeWhenContextDone(ctx context.Context, closer io.Closer) func() {
	return runWhenContextDone(ctx, func() {
		closeQuietly(closer)
	})
}

func runWhenContextDone(ctx context.Context, fn func()) func() {
	stop := context.AfterFunc(ctx, fn)
	return func() {
		_ = stop()
	}
}

func closeQuietly(closer io.Closer) {
	if closer == nil {
		return
	}
	_ = closer.Close()
}

func setDeadlineQuietly(conn net.Conn, deadline time.Time) {
	if conn == nil {
		return
	}
	_ = conn.SetDeadline(deadline)
}

func writeSOCKSReplyQuietly(writer io.Writer, code byte) {
	_ = writeSocksReply(writer, code)
}
