// Package proxy serves a local SOCKS5 no-auth listener, relays TCP and UDP
// through an Upstream, and reports connection events. It assumes a trusted
// host.
package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/rbee3u/efanadp/internal/datagram"
	"github.com/rbee3u/efanadp/internal/human"
	"github.com/rbee3u/efanadp/internal/sniff"
)

// Upstream opens X365-carried streams for SOCKS destinations. Implementations
// must honor context cancellation and return streams whose Close unblocks I/O.
// Normal closure must use standard stream errors (such as io.ErrClosedPipe or
// net.ErrClosed); carrier-specific errors are translated by the implementation.
type Upstream interface {
	OpenTCP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error)
	OpenUDP(ctx context.Context, destHost string, destPort int) (datagram.Conn, error)
}

// Logger receives relay messages from concurrent connection goroutines.
// Implementations must be concurrency-safe and return promptly.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

// Config supplies the SOCKS server's upstream and optional observability hooks.
// Logger and OnConnection can be called concurrently and must not block relay
// goroutines for long-running work.
type Config struct {
	// Upstream is required before the first SOCKS request is served.
	Upstream Upstream

	// HandshakeTimeout bounds the client greeting and request. Non-positive
	// values disable that deadline.
	HandshakeTimeout time.Duration

	// ShadowsocksSniffConfigs affects target labels only, never routing.
	ShadowsocksSniffConfigs []sniff.ShadowsocksConfig

	// Logger is optional; nil disables relay logging.
	Logger Logger

	// OnConnection optionally receives synchronous lifecycle events. Event.Stats
	// remains live after delivery and is safe to read concurrently.
	OnConnection func(Event)

	// UDPTargetIdleTimeout bounds how long an unused UDP destination keeps its
	// upstream stream cached. Zero selects DefaultUDPTargetIdleTimeout; a
	// negative value disables expiry.
	UDPTargetIdleTimeout time.Duration

	listenUDP func(*net.UDPAddr) (udpPacketConn, error)
}

func (cfg Config) udpTargetIdleTimeout() time.Duration {
	if cfg.UDPTargetIdleTimeout == 0 {
		return DefaultUDPTargetIdleTimeout
	}
	return cfg.UDPTargetIdleTimeout
}

// ServeSOCKS listens and blocks until ctx is canceled or the accept loop fails.
// Cancellation closes the listener and active clients, then waits for their
// handlers to return.
func ServeSOCKS(ctx context.Context, listen string, cfg Config) error {
	var listenConfig net.ListenConfig
	ln, err := listenConfig.Listen(ctx, "tcp", listen)
	if err != nil {
		return fmt.Errorf("listen socks5: %w", err)
	}
	return serveSOCKS(ctx, ln, cfg)
}

func serveSOCKS(ctx context.Context, ln net.Listener, cfg Config) error {
	defer closeQuietly(ln)

	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopClosingListener := closeWhenContextDone(serveCtx, ln)
	defer stopClosingListener()

	var handlers sync.WaitGroup
	err := acceptSOCKSConnections(serveCtx, ln, cfg, &handlers)
	cancel()
	closeQuietly(ln)
	handlers.Wait()
	if errors.Is(err, errAcceptLoopClosed) {
		return nil
	}
	return err
}

var errAcceptLoopClosed = errors.New("accept loop closed")

const (
	acceptRetryDelayMin = 5 * time.Millisecond
	acceptRetryDelayMax = time.Second
)

func acceptSOCKSConnections(ctx context.Context, ln net.Listener, cfg Config, handlers *sync.WaitGroup) error {
	var retryDelay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return errAcceptLoopClosed
			}
			if !isRecoverableAcceptError(err) {
				return err
			}
			retryDelay = nextAcceptRetryDelay(retryDelay)
			cfg.warnf("[efanadp] accept failed, retrying in %s: %v", human.ShortDuration(retryDelay), err)
			if !waitBeforeAcceptRetry(ctx, retryDelay) {
				return errAcceptLoopClosed
			}
			continue
		}
		retryDelay = 0
		handlers.Go(func() {
			if err := handleSOCKSConn(ctx, conn, cfg); err != nil && !isBenignClientError(err) {
				cfg.warnf("[efanadp] client failed: %v", err)
			}
		})
	}
}

// Resource exhaustion can clear as connections close; it must not stop an
// otherwise healthy listener.
func isRecoverableAcceptError(err error) bool {
	return errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) ||
		errors.Is(err, syscall.ENOBUFS) ||
		errors.Is(err, syscall.ENOMEM)
}

func nextAcceptRetryDelay(current time.Duration) time.Duration {
	if current <= 0 {
		return acceptRetryDelayMin
	}
	return min(2*current, acceptRetryDelayMax)
}

func waitBeforeAcceptRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func handleSOCKSConn(ctx context.Context, client net.Conn, cfg Config) error {
	defer closeQuietly(client)
	stopClosingClient := closeWhenContextDone(ctx, client)
	defer stopClosingClient()

	reader := bufio.NewReader(client)
	request, err := negotiateSOCKSRequest(client, reader, cfg.HandshakeTimeout)
	if err != nil {
		return err
	}
	switch request.Command {
	case socksCommandConnect:
		return handleSOCKSConnect(ctx, client, reader, request, cfg)
	case socksCommandUDPAssociate:
		return handleSOCKSUDPAssociate(ctx, client, reader, request, cfg)
	default:
		return fmt.Errorf("unsupported SOCKS5 command: %s", socksCommandName(request.Command))
	}
}

func handleSOCKSConnect(ctx context.Context, client net.Conn, reader *bufio.Reader, request socksRequest, cfg Config) error {
	if isFakeIPHost(request.Host) {
		writeSOCKSReplyQuietly(client, socksReplyHostUnreachable)
		return fmt.Errorf("SOCKS5 request got mihomo fake-ip %s; use socks5h:// so the proxy receives the domain", request.Host)
	}

	tracker := newConnectionTracker(cfg, "tcp", client.RemoteAddr().String(), request.address())
	tracker.open()

	upstream, err := cfg.openTCP(ctx, request.Host, request.Port)
	if err != nil {
		writeSOCKSReplyQuietly(client, socksReplyConnectionRefused)
		tracker.close(err)
		return nil
	}
	defer closeQuietly(upstream)
	stopClosingUpstream := closeWhenContextDone(ctx, upstream)
	defer stopClosingUpstream()

	writeSOCKSReplyQuietly(client, socksReplySucceeded)
	err = relayTCP(ctx, &bufferedConn{Conn: client, reader: reader}, upstream, tracker.stats, uploadObserverFor(cfg, request, tracker))
	tracker.close(err)
	return nil
}

func negotiateSOCKSRequest(client net.Conn, reader *bufio.Reader, handshakeTimeout time.Duration) (socksRequest, error) {
	if handshakeTimeout > 0 {
		setDeadlineQuietly(client, time.Now().Add(handshakeTimeout))
	}
	if err := handshakeSOCKS(reader, client); err != nil {
		return socksRequest{}, fmt.Errorf("SOCKS5 handshake: %w", err)
	}
	request, err := readSOCKSRequest(reader, client)
	if err != nil {
		return socksRequest{}, fmt.Errorf("SOCKS5 request: %w", err)
	}
	if handshakeTimeout > 0 {
		setDeadlineQuietly(client, time.Time{})
	}
	return request, nil
}

func uploadObserverFor(cfg Config, target socksRequest, tracker *connectionTracker) func([]byte) {
	sniffer := sniff.NewShadowsocksTCPSniffer(cfg.ShadowsocksSniffConfigs, target.Host, target.Port)
	if sniffer == nil {
		return nil
	}
	return func(p []byte) {
		innerTarget, ok := sniffer.Observe(p)
		if !ok {
			return
		}
		nextTarget := formatTargetChain(target.address(), innerTarget)
		tracker.updateTarget(nextTarget)
	}
}

func (cfg Config) infof(format string, args ...any) {
	if cfg.Logger == nil {
		return
	}
	cfg.Logger.Infof(format, args...)
}

func (cfg Config) warnf(format string, args ...any) {
	if cfg.Logger == nil {
		return
	}
	cfg.Logger.Warnf(format, args...)
}

func (cfg Config) emitConnection(event Event) {
	if cfg.OnConnection == nil {
		return
	}
	cfg.OnConnection(event)
}

func (cfg Config) openTCP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("proxy upstream is required")
	}
	return cfg.Upstream.OpenTCP(ctx, destHost, destPort)
}

func (cfg Config) openUDP(ctx context.Context, destHost string, destPort int) (datagram.Conn, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("proxy upstream is required")
	}
	return cfg.Upstream.OpenUDP(ctx, destHost, destPort)
}

func (cfg Config) bindUDP(addr *net.UDPAddr) (udpPacketConn, error) {
	if cfg.listenUDP != nil {
		return cfg.listenUDP(addr)
	}
	return net.ListenUDP("udp", addr)
}

func isBenignClientError(err error) bool {
	if errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
