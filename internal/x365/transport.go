package x365

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

func openXHTTPRealityStream(ctx context.Context, node Node, timeout time.Duration) (io.ReadWriteCloser, error) {
	if node.Path == "" {
		return nil, errors.New("node path is empty")
	}
	realityConfig, err := buildRealityConfig(node)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	streamCtx, cancel := context.WithCancel(ctx)
	req, err := newXHTTPRealityRequest(streamCtx, node, pr)
	if err != nil {
		cancel()
		closeQuietly(pr)
		closeQuietly(pw)
		return nil, err
	}

	conns := &streamConns{}
	transport := newXHTTPRealityTransport(node, timeout, realityConfig, conns)
	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
		cancel:        cancel,
		closeConns:    conns.closeAll,
	}
	stream.startResponse(transport, req, pr)

	return stream, nil
}

// streamConns owns every socket dialed for a stream. CloseIdleConnections can
// miss sockets still busy in RoundTrip, leaving the per-stream transport and
// its read loop alive.
type streamConns struct {
	mu     sync.Mutex
	conns  []net.Conn
	closed bool
}

// adopt records conn, or closes it right away when the stream is already gone.
// http2 may dial more than once for a single request, so every conn is kept.
func (sc *streamConns) adopt(conn net.Conn) {
	sc.mu.Lock()
	if sc.closed {
		sc.mu.Unlock()
		closeQuietly(conn)
		return
	}
	sc.conns = append(sc.conns, conn)
	sc.mu.Unlock()
}

func (sc *streamConns) closeAll() {
	sc.mu.Lock()
	sc.closed = true
	conns := sc.conns
	sc.conns = nil
	sc.mu.Unlock()

	for _, conn := range conns {
		closeQuietly(conn)
	}
}

func newXHTTPRealityRequest(ctx context.Context, node Node, body io.Reader) (*http.Request, error) {
	reqURL := url.URL{
		Scheme: "https",
		Host:   node.Host,
		Path:   node.Path,
	}
	if reqURL.Host == "" {
		reqURL.Host = node.SNI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("User-Agent", chromeUserAgent)
	if node.Host != "" {
		req.Host = node.Host
		req.Header.Set("Referer", "https://"+node.Host+node.Path)
	}
	return req, nil
}

func newXHTTPRealityTransport(
	node Node,
	timeout time.Duration,
	realityConfig *realityClientConfig,
	conns *streamConns,
) *http2.Transport {
	return &http2.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			if timeout > 0 {
				dialer.Timeout = timeout
			}
			raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(node.Server, strconv.Itoa(node.Port)))
			if err != nil {
				return nil, fmt.Errorf("tcp dial: %w", err)
			}
			conn, err := dialReality(ctx, raw, realityConfig)
			if err != nil {
				closeQuietly(raw)
				return nil, fmt.Errorf("reality handshake: %w", err)
			}
			conns.adopt(conn)
			return conn, nil
		},
	}
}

const chromeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

const maxErrorResponseDrain = 1 << 20

func roundTripResponseBody(transport http.RoundTripper, req *http.Request) (io.ReadCloser, error) {
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}
	defer closeQuietly(resp.Body)
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorResponseDrain)); err != nil {
		return nil, fmt.Errorf("read x365 error response: %w", err)
	}
	return nil, fmt.Errorf("x365 server returned HTTP %d", resp.StatusCode)
}

type httpStream struct {
	requestWriter *io.PipeWriter
	responseDone  chan struct{}
	cancel        context.CancelFunc
	closeConns    func()

	mu        sync.Mutex
	closeOnce sync.Once
	isClosed  bool
	body      io.ReadCloser
	err       error
}

var _ io.ReadWriteCloser = (*httpStream)(nil)

func (hs *httpStream) Write(p []byte) (int, error) {
	return hs.requestWriter.Write(p)
}

func (hs *httpStream) Read(p []byte) (int, error) {
	body, err := hs.waitResponse()
	if err != nil {
		return 0, err
	}
	if body == nil {
		return 0, io.ErrClosedPipe
	}
	n, err := body.Read(p)
	// HTTP/2 breaks a closed response body's read pipe with an unexported
	// sentinel. Translate that exact error here so callers only need the
	// standard stream contract, retaining the original diagnostic and chain.
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if cause.Error() == "http2: response body closed" {
			return n, fmt.Errorf("%w: %w", net.ErrClosed, err)
		}
	}
	return n, err
}

func (hs *httpStream) Close() error {
	hs.closeOnce.Do(func() {
		if hs.cancel != nil {
			hs.cancel()
		}
		closeQuietly(hs.requestWriter)

		hs.mu.Lock()
		hs.isClosed = true
		body := hs.body
		hs.mu.Unlock()
		if body != nil {
			closeQuietly(body)
		}
		// Explicit socket closure also releases the HTTP/2 read loop.
		if hs.closeConns != nil {
			hs.closeConns()
		}
	})
	return nil
}

func (hs *httpStream) waitResponse() (io.ReadCloser, error) {
	<-hs.responseDone

	hs.mu.Lock()
	defer hs.mu.Unlock()
	return hs.body, hs.err
}

func (hs *httpStream) setResponse(body io.ReadCloser, err error) {
	hs.mu.Lock()
	hs.body = body
	hs.err = err
	closed := hs.isClosed
	hs.mu.Unlock()

	if closed && body != nil {
		closeQuietly(body)
	}
	close(hs.responseDone)
}

func (hs *httpStream) startResponse(
	transport http.RoundTripper,
	req *http.Request,
	requestReader *io.PipeReader,
) {
	go func() {
		body, err := roundTripResponseBody(transport, req)
		if err != nil {
			err = fmt.Errorf("http2 post: %w", err)
			if requestReader != nil {
				_ = requestReader.CloseWithError(err)
			}
		}
		hs.setResponse(body, err)
	}()
}
