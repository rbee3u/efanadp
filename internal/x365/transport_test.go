package x365

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestNewXHTTPRealityRequestUsesObservedHeaders(t *testing.T) {
	req, err := newXHTTPRealityRequest(t.Context(), Node{Host: "example.net", Path: "/x365"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "https://example.net/x365" {
		t.Fatalf("url=%q, want https://example.net/x365", got)
	}
	if req.Host != "example.net" {
		t.Fatalf("host override=%q, want example.net", req.Host)
	}
	if got := req.Header.Get("Content-Type"); got != "application/grpc" {
		t.Fatalf("content type=%q, want application/grpc", got)
	}
	if got := req.Header.Get("User-Agent"); got != chromeUserAgent {
		t.Fatalf("user agent=%q, want chrome user agent", got)
	}
	if got := req.Header.Get("Referer"); got != "https://example.net/x365" {
		t.Fatalf("referer=%q, want observed referer", got)
	}
}

func TestNewXHTTPRealityRequestFallsBackToSNIHost(t *testing.T) {
	req, err := newXHTTPRealityRequest(t.Context(), Node{SNI: "example.org", Path: "/x365"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "https://example.org/x365" {
		t.Fatalf("url=%q, want SNI host fallback", got)
	}
	if req.Host != "example.org" {
		t.Fatalf("host=%q, want SNI host fallback", req.Host)
	}
	if got := req.Header.Get("Referer"); got != "" {
		t.Fatalf("referer=%q, want none without node host", got)
	}
}

func TestHTTPStreamCloseCancelsPendingRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	pr, pw := io.Pipe()
	defer func() {
		closeQuietly(pr)
	}()

	connsClosed := make(chan struct{}, 1)
	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
		cancel:        cancel,
		closeConns: func() {
			connsClosed <- struct{}{}
		},
	}

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context err=%v, want canceled", ctx.Err())
	}
	select {
	case <-connsClosed:
	default:
		t.Fatal("expected Close to close the stream connections")
	}
}

func TestStreamConnsCloseAllClosesAdoptedConns(t *testing.T) {
	conns := &streamConns{}
	first, second := &fakeStreamConn{}, &fakeStreamConn{}
	conns.adopt(first)
	conns.adopt(second)

	conns.closeAll()

	if !first.isClosed() || !second.isClosed() {
		t.Fatalf("closed first=%v second=%v, want both closed", first.isClosed(), second.isClosed())
	}
}

func TestStreamConnsAdoptAfterCloseClosesImmediately(t *testing.T) {
	conns := &streamConns{}
	conns.closeAll()

	// A dial that finishes after the stream is gone must not park a live socket.
	late := &fakeStreamConn{}
	conns.adopt(late)

	if !late.isClosed() {
		t.Fatal("expected a conn adopted after closeAll to be closed immediately")
	}
}

// Exercise real httpStream ownership through local h2c, replacing only the
// REALITY dial with plain TCP.
func TestHTTPStreamCloseReleasesServerConnections(t *testing.T) {
	const streams = 8

	server := startEchoH2CServer(t)
	for i := range streams {
		stream := server.openStream(t)
		if _, err := stream.Write([]byte("ping")); err != nil {
			t.Fatalf("stream %d write: %v", i, err)
		}
		// Read the echo so the stream is fully established server-side before
		// it is closed; a leak is only observable once a connection exists.
		echo := make([]byte, len("ping"))
		if _, err := io.ReadFull(stream, echo); err != nil {
			t.Fatalf("stream %d read: %v", i, err)
		}
		if err := stream.Close(); err != nil {
			t.Fatalf("stream %d close: %v", i, err)
		}
	}

	if err := server.waitForLiveConns(0); err != nil {
		t.Fatalf("after closing %d streams: %v", streams, err)
	}
}

func TestHTTPStreamClosedResponseUsesStandardError(t *testing.T) {
	server := startEchoH2CServer(t)
	stream := server.openStream(t)
	defer closeQuietly(stream)
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("ping"))
	if _, err := io.ReadFull(stream, buf); err != nil {
		t.Fatal(err)
	}
	// Exercise the real HTTP/2 sentinel without also canceling the stream's
	// context, which can independently unblock Read with context.Canceled.
	body, err := stream.waitResponse()
	if err != nil {
		t.Fatal(err)
	}
	closeQuietly(body)
	if _, err := stream.Read(buf); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Read after response close=%v, want net.ErrClosed", err)
	}
}

func TestHTTPStreamReadPreservesErrorAndBytes(t *testing.T) {
	closed := errors.New("http2: response body closed")
	tests := []struct {
		name   string
		err    error
		closed bool
	}{
		{name: "closed", err: closed, closed: true},
		{name: "wrapped closed", err: fmt.Errorf("response read: %w", closed), closed: true},
		{name: "unrelated failure", err: errors.New("response failed")},
		{name: "similar diagnostic", err: errors.New("unexpected http2: response body closed while opening")},
		{name: "eof", err: io.EOF},
		{name: "no error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := &httpStream{responseDone: make(chan struct{})}
			stream.setResponse(io.NopCloser(&responseErrorReader{err: tt.err}), nil)
			buf := make([]byte, 1)
			n, err := stream.Read(buf)
			if n != 1 || buf[0] != 'x' {
				t.Fatalf("Read=(%d, %q), want (1, x)", n, buf)
			}
			if errors.Is(err, net.ErrClosed) != tt.closed {
				t.Fatalf("Read error=%v, closed=%v", err, tt.closed)
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("Read error=%v, lost original %v", err, tt.err)
			}
		})
	}
}

type responseErrorReader struct{ err error }

func (r *responseErrorReader) Read(p []byte) (int, error) {
	return copy(p, "x"), r.err
}

type echoH2CServer struct {
	addr string
	live atomic.Int64
}

func (s *echoH2CServer) openStream(tb testing.TB) *httpStream {
	tb.Helper()
	pr, pw := io.Pipe()
	streamCtx, cancel := context.WithCancel(tb.Context())
	req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, "http://"+s.addr+"/x365", pr)
	if err != nil {
		cancel()
		tb.Fatal(err)
	}

	conns := &streamConns{}
	transport := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, _, _ string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "tcp", s.addr)
			if err != nil {
				return nil, err
			}
			conns.adopt(conn)
			return conn, nil
		},
	}
	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
		cancel:        cancel,
		closeConns:    conns.closeAll,
	}
	stream.startResponse(transport, req, pr)
	return stream
}

func (s *echoH2CServer) waitForLiveConns(want int64) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := s.live.Load()
		if got == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("live server connections=%d, want %d", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func startEchoH2CServer(tb testing.TB) *echoH2CServer {
	tb.Helper()
	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(tb.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		closeQuietly(listener)
	})

	server := &echoH2CServer{addr: listener.Addr().String()}
	h2Server := &http2.Server{}
	handler := http.HandlerFunc(echoH2CHandler)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			server.live.Add(1)
			tracked := &trackedServerConn{Conn: conn, live: &server.live}
			go h2Server.ServeConn(tracked, &http2.ServeConnOpts{Handler: handler})
		}
	}()
	return server
}

func echoH2CHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

type trackedServerConn struct {
	net.Conn
	live      *atomic.Int64
	closeOnce sync.Once
}

func (c *trackedServerConn) Close() error {
	c.closeOnce.Do(func() {
		c.live.Add(-1)
	})
	return c.Conn.Close()
}

type fakeStreamConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *fakeStreamConn) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *fakeStreamConn) isClosed() bool {
	return c.closed.Load()
}

func TestHTTPStreamCloseDoesNotWaitForResponse(t *testing.T) {
	pr, pw := io.Pipe()
	defer closeQuietly(pr)

	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
	}

	done := make(chan error, 1)
	go func() {
		done <- stream.Close()
	}()

	if err := receiveWithin(t, done, "Close without response"); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPStreamCloseClosesReadyResponse(t *testing.T) {
	pr, pw := io.Pipe()
	defer closeQuietly(pr)
	body := &closeNotifyingBody{closed: make(chan struct{})}
	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
	}
	stream.setResponse(body, nil)

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("expected Close to close a ready response body")
	}
}

func TestRoundTripResponseBodyClosesNonOKResponse(t *testing.T) {
	body := &closeNotifyingBody{
		reader: strings.NewReader("error response body"),
		closed: make(chan struct{}),
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: body}, nil
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.org/x365", nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := roundTripResponseBody(transport, req)
	if err == nil {
		closeQuietly(got)
		t.Fatal("expected HTTP status error")
	}
	if got != nil {
		closeQuietly(got)
		t.Fatal("expected no response body on HTTP status error")
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("expected non-OK response body to close")
	}
	if body.reader.Len() != 0 {
		t.Fatalf("non-OK response body bytes left=%d, want 0", body.reader.Len())
	}
}

func TestRoundTripResponseBodyLimitsNonOKResponseDrain(t *testing.T) {
	body := &closeNotifyingBody{
		reader: strings.NewReader(strings.Repeat("x", maxErrorResponseDrain+1)),
		closed: make(chan struct{}),
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: body}, nil
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.org/x365", nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := roundTripResponseBody(transport, req)
	if err == nil {
		closeQuietly(got)
		t.Fatal("expected HTTP status error")
	}
	if got != nil {
		closeQuietly(got)
		t.Fatal("expected no response body on HTTP status error")
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("expected non-OK response body to close")
	}
	if body.reader.Len() != 1 {
		t.Fatalf("non-OK response body bytes left=%d, want 1", body.reader.Len())
	}
}

type closeNotifyingBody struct {
	reader *strings.Reader
	closed chan struct{}
}

func (b *closeNotifyingBody) Read(p []byte) (int, error) {
	if b.reader != nil {
		return b.reader.Read(p)
	}
	return 0, io.EOF
}

func (b *closeNotifyingBody) Close() error {
	close(b.closed)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
