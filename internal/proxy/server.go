package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rbee3u/efanadp/internal/human"
	"github.com/rbee3u/efanadp/internal/x365"
)

type Upstream interface {
	OpenTCP(context.Context, string, int) (io.ReadWriteCloser, error)
	OpenUDP(context.Context, string, int) (io.ReadWriteCloser, error)
}

type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
}

type Config struct {
	Upstream                Upstream
	HandshakeTimeout        time.Duration
	ShadowsocksSniffConfigs []x365.ShadowsocksSniffConfig
	Logger                  Logger
	OnConnection            func(Event)

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

func ServeSOCKS(ctx context.Context, listen string, cfg Config) error {
	var listenConfig net.ListenConfig
	ln, err := listenConfig.Listen(ctx, "tcp", listen)
	if err != nil {
		return fmt.Errorf("listen socks5: %w", err)
	}
	return serveSOCKS(ctx, ln, cfg)
}

func serveSOCKS(ctx context.Context, ln net.Listener, cfg Config) error {
	defer func() {
		closeQuietly(ln)
	}()

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

// Accept retry pacing after a recoverable failure: short enough that the proxy
// resumes as soon as capacity frees up, capped so a persistent shortage does not
// spin.
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
			if err := handleSOCKSConn(ctx, conn, cfg); err != nil {
				if isBenignClientError(err) {
					return
				}
				cfg.warnf("[efanadp] client failed: %v", err)
			}
		})
	}
}

// isRecoverableAcceptError reports whether the accept loop should keep serving.
//
// Descriptor and buffer exhaustion is the realistic failure here, because every
// proxied connection holds an upstream socket and every cached UDP destination
// holds another, so a busy client can push the process to its limit while the
// listener itself stays healthy. Returning the error instead would stop the
// whole SOCKS service over a shortage that clears on its own as connections
// close, leaving the user to notice and restart it.
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

// waitBeforeAcceptRetry reports false when the service stopped while waiting.
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
	defer func() {
		closeQuietly(client)
	}()
	stopClosingClient := closeWhenContextDone(ctx, client)
	defer stopClosingClient()

	reader := bufio.NewReader(client)
	request, err := readSOCKSRequest(client, reader, cfg.HandshakeTimeout)
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
		writeSOCKSReplyQuietly(client, 4)
		return fmt.Errorf("SOCKS5 request got mihomo fake-ip %s; use socks5h:// so the proxy receives the domain", request.Host)
	}

	tracker := newConnectionTracker(cfg, client.RemoteAddr().String(), request.Address)
	tracker.open()

	upstream, err := cfg.dialMultipath(ctx, request.Host, request.Port)
	if err != nil {
		writeSOCKSReplyQuietly(client, 5)
		tracker.close(err)
		return nil
	}
	defer func() {
		closeQuietly(upstream)
	}()
	stopClosingUpstream := closeWhenContextDone(ctx, upstream)
	defer stopClosingUpstream()

	writeSOCKSReplyQuietly(client, 0)
	err = relayMultipath(ctx, &bufferedConn{Conn: client, reader: reader}, upstream, tracker.stats, uploadObserverFor(cfg, request, tracker))
	tracker.close(err)
	return nil
}

func readSOCKSRequest(client net.Conn, reader *bufio.Reader, handshakeTimeout time.Duration) (socksRequest, error) {
	if handshakeTimeout > 0 {
		setDeadlineQuietly(client, time.Now().Add(handshakeTimeout))
	}
	if err := socksHandshake(reader, client); err != nil {
		return socksRequest{}, fmt.Errorf("SOCKS5 handshake: %w", err)
	}
	request, err := socksReadRequest(reader, client)
	if err != nil {
		return socksRequest{}, fmt.Errorf("SOCKS5 request: %w", err)
	}
	if handshakeTimeout > 0 {
		setDeadlineQuietly(client, time.Time{})
	}
	return request, nil
}

func uploadObserverFor(cfg Config, target socksRequest, tracker *connectionTracker) func([]byte) {
	sniffer := x365.NewShadowsocksTargetSniffer(cfg.ShadowsocksSniffConfigs, target.Host, target.Port)
	if sniffer == nil {
		return nil
	}
	return func(p []byte) {
		innerTarget, ok := sniffer.Observe(p)
		if !ok {
			return
		}
		nextTarget := x365.FormatTargetChain(target.Address, innerTarget)
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

func (cfg Config) dialMultipath(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	if cfg.Upstream == nil {
		return nil, errors.New("proxy upstream is required")
	}
	return cfg.Upstream.OpenTCP(ctx, destHost, destPort)
}

func (cfg Config) dialUDP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
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
	switch {
	case errors.Is(err, io.EOF):
		return true
	case errors.Is(err, io.ErrClosedPipe):
		return true
	case errors.Is(err, io.ErrUnexpectedEOF):
		return true
	case errors.Is(err, net.ErrClosed):
		return true
	}
	if strings.Contains(err.Error(), "http2: response body closed") {
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
