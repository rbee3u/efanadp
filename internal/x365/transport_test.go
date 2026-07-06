package x365

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
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

	idleClosed := make(chan struct{}, 1)
	stream := &httpStream{
		requestWriter: pw,
		responseDone:  make(chan struct{}),
		cancel:        cancel,
		closeIdle: func() {
			idleClosed <- struct{}{}
		},
	}

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context err=%v, want canceled", ctx.Err())
	}
	select {
	case <-idleClosed:
	default:
		t.Fatal("expected Close to close idle connections")
	}
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
