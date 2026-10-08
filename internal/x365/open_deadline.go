package x365

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

var errOpenTimeout = errors.New("x365 open handshake timed out")

// handshakeWithDeadline closes the stream on timeout to unblock handshake I/O.
// TCP includes the HTTP/X365 response; UDP defers that read. A successful
// handshake takes ownership; a failed one must close the stream.
func handshakeWithDeadline[T io.Closer](
	ctx context.Context,
	timeout time.Duration,
	stream io.ReadWriteCloser,
	handshake func() (T, error),
) (T, error) {
	var zero T
	if timeout <= 0 {
		return handshake()
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stopClosing := context.AfterFunc(deadlineCtx, func() {
		closeQuietly(stream)
	})

	conn, err := handshake()
	// stopClosing reports false once the closer has started, so from here on the
	// stream must be treated as closed no matter what handshake returned.
	closingStarted := !stopClosing()
	timedOut := closingStarted && ctx.Err() == nil && errors.Is(deadlineCtx.Err(), context.DeadlineExceeded)
	if closingStarted {
		if err == nil {
			// Even a successful handshake now wraps a closing stream. Release
			// wrappers such as the multipath ACK goroutine before returning.
			closeQuietly(conn)
		}
		if timedOut {
			return zero, fmt.Errorf("%w after %s", errOpenTimeout, timeout)
		}
		if parentErr := ctx.Err(); parentErr != nil {
			return zero, parentErr
		}
		if err == nil {
			err = deadlineCtx.Err()
		}
	}
	if err != nil {
		return zero, err
	}
	return conn, nil
}
