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
	"time"

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
	listenUDP               func(*net.UDPAddr) (udpPacketConn, error)
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

func acceptSOCKSConnections(ctx context.Context, ln net.Listener, cfg Config, handlers *sync.WaitGroup) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return acceptLoopError(ctx, err)
		}
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

func acceptLoopError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
		return errAcceptLoopClosed
	}
	return err
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
