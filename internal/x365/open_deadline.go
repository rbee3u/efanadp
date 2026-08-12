package x365

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// errOpenTimeout reports that the X365 open handshake exceeded its deadline.
var errOpenTimeout = errors.New("x365 open handshake timed out")

// handshakeWithDeadline bounds the blocking steps that turn a fresh xhttp
// stream into a usable X365 connection: the REALITY handshake, the HTTP/2
// response headers, the X365 request write, and the X365 response read.
//
// Only the TCP dial is bounded otherwise. net/http2 has no response-header
// timeout, so a server that completes TCP and TLS but never answers leaves the
// caller blocked in readX365OKResponse with no deadline. For TCP that pins the
// SOCKS handler, its client conn, a goroutine, and the stream until the whole
// service stops, which is a realistic outcome on a blackholed or throttled path.
//
// The deadline closes the stream, which cancels the request context and the
// request pipe; that unblocks a pending write, response wait, or body read
// alike. handshake owns the stream on success and is responsible for closing it
// on failure, exactly as it is without a deadline.
func handshakeWithDeadline(
	ctx context.Context,
	timeout time.Duration,
	stream io.ReadWriteCloser,
	handshake func() (io.ReadWriteCloser, error),
) (io.ReadWriteCloser, error) {
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
	if err != nil {
		// A parent cancellation is reported as itself; only the added deadline
		// is reported as a timeout.
		if timedOut {
			return nil, fmt.Errorf("%w after %s", errOpenTimeout, timeout)
		}
		if closingStarted {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		return nil, err
	}
	if closingStarted {
		// A deadline or parent cancellation landed in the gap between a successful
		// handshake and stopping the closer, so conn wraps a stream that is already
		// closing. Returning it would surface the cancellation later as an opaque
		// relay error. Closing conn also releases wrappers such as the multipath ACK
		// goroutine that the stream closer does not know about.
		closeQuietly(conn)
		if timedOut {
			return nil, fmt.Errorf("%w after %s", errOpenTimeout, timeout)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, deadlineCtx.Err()
	}
	return conn, nil
}
