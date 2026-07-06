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

	transport := newXHTTPRealityTransport(node, timeout, realityConfig)
	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
		cancel:        cancel,
		closeIdle:     transport.CloseIdleConnections,
	}
	stream.startResponse(transport, req, pr)

	return stream, nil
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
	if node.Host != "" {
		req.Host = node.Host
	}
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("User-Agent", chromeUserAgent)
	if node.Host != "" {
		req.Header.Set("Referer", "https://"+node.Host+node.Path)
	}
	return req, nil
}

func newXHTTPRealityTransport(
	node Node,
	timeout time.Duration,
	realityConfig *realityClientConfig,
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
	if resp.StatusCode != http.StatusOK {
		if _, drainErr := io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorResponseDrain)); drainErr != nil {
			closeQuietly(resp.Body)
			return nil, fmt.Errorf("read x365 error response: %w", drainErr)
		}
		closeQuietly(resp.Body)
		return nil, fmt.Errorf("x365 server returned HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

type httpStream struct {
	requestWriter *io.PipeWriter
	responseDone  chan struct{}
	cancel        context.CancelFunc
	closeIdle     func()

	mu        sync.Mutex
	closeOnce sync.Once
	isClosed  bool
	body      io.ReadCloser
	err       error
}

var _ io.ReadWriteCloser = (*httpStream)(nil)

func (s *httpStream) Write(p []byte) (int, error) {
	return s.requestWriter.Write(p)
}

func (s *httpStream) Read(p []byte) (int, error) {
	if err := s.waitResponse(); err != nil {
		return 0, err
	}
	body := s.responseBody()
	if body == nil {
		return 0, io.ErrClosedPipe
	}
	return body.Read(p)
}

func (s *httpStream) Close() error {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		closeQuietly(s.requestWriter)

		s.mu.Lock()
		s.isClosed = true
		body := s.body
		s.mu.Unlock()
		if body != nil {
			closeQuietly(body)
		}
		if s.closeIdle != nil {
			s.closeIdle()
		}
	})
	return nil
}

func (s *httpStream) waitResponse() error {
	<-s.responseDone

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *httpStream) setResponse(body io.ReadCloser, err error) {
	s.mu.Lock()
	s.body = body
	s.err = err
	closed := s.isClosed
	s.mu.Unlock()

	if closed && body != nil {
		closeQuietly(body)
	}
	close(s.responseDone)
}

func (s *httpStream) responseBody() io.ReadCloser {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body
}

func (s *httpStream) startResponse(
	transport http.RoundTripper,
	req *http.Request,
	requestReader *io.PipeReader,
) {
	go func() {
		body, err := roundTripResponseBody(transport, req)
		if err != nil {
			err = fmt.Errorf("http2 post: %w", err)
			closePipeReaderWithError(requestReader, err)
			s.setResponse(nil, err)
			return
		}
		s.setResponse(body, nil)
	}()
}
