package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/rbee3u/efanadp/internal/datagram"
)

func TestServeSOCKSUsesConfiguredLogger(t *testing.T) {
	ln := newPipeListener()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	logCh := make(chan string, 1)
	errCh := make(chan error, 1)
	cfg := Config{
		Logger: testLogger{log: func(_ string, format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if strings.Contains(msg, "client failed") {
				logCh <- msg
			}
		}},
	}

	go func() {
		errCh <- serveSOCKS(ctx, ln, cfg)
	}()

	conn, err := ln.Dial()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte{5, 1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	msg := receiveWithin(t, logCh, "configured logger")
	if !strings.Contains(msg, "no-auth") {
		t.Fatalf("log message %q does not mention no-auth failure", msg)
	}

	cancel()
	if err := receiveWithin(t, errCh, "SOCKS server shutdown"); err != nil {
		t.Fatalf("serveSOCKS returned %v", err)
	}
}

func TestServeSOCKSKeepsServingAfterDescriptorExhaustion(t *testing.T) {
	ln := newPipeListener()
	ln.failNextAccepts(&net.OpError{Op: "accept", Err: syscall.EMFILE}, 3)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	warnCh := make(chan string, 8)
	errCh := make(chan error, 1)
	cfg := Config{
		Logger: testLogger{log: func(_ string, format string, args ...any) {
			msg := fmt.Sprintf(format, args...)
			if strings.Contains(msg, "accept failed") {
				select {
				case warnCh <- msg:
				default:
				}
			}
		}},
	}

	go func() {
		errCh <- serveSOCKS(ctx, ln, cfg)
	}()

	msg := receiveWithin(t, warnCh, "accept retry warning")
	if !strings.Contains(msg, "retrying in") {
		t.Fatalf("warning %q does not report a retry delay", msg)
	}

	// The listener has to still be accepting once the shortage clears.
	conn, err := ln.Dial()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("handshake after accept retry: %v", err)
	}
	if reply[0] != 5 || reply[1] != 0 {
		t.Fatalf("handshake reply=%v, want no-auth accepted", reply)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	cancel()
	if err := receiveWithin(t, errCh, "SOCKS server shutdown"); err != nil {
		t.Fatalf("serveSOCKS returned %v", err)
	}
}

func TestServeSOCKSReturnsUnrecoverableAcceptError(t *testing.T) {
	ln := newPipeListener()
	acceptErr := &net.OpError{Op: "accept", Err: syscall.EINVAL}
	ln.failNextAccepts(acceptErr, 1)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- serveSOCKS(ctx, ln, Config{})
	}()

	if err := receiveWithin(t, errCh, "accept failure"); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("serveSOCKS returned %v, want EINVAL", err)
	}
}

func TestServeSOCKSWaitsForActiveHandlersOnCancel(t *testing.T) {
	ln := newPipeListener()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stream := newCloseBlockingStream()
	defer stream.releaseClose()

	errCh := make(chan error, 1)
	cfg := Config{
		Upstream: testUpstream{
			openTCP: func(context.Context, string, int) (io.ReadWriteCloser, error) {
				return stream, nil
			},
		},
	}
	go func() {
		errCh <- serveSOCKS(ctx, ln, cfg)
	}()

	conn, err := ln.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{5, 0}) {
		t.Fatalf("handshake reply=%v, want [5 0]", reply)
	}
	request := append([]byte{5, 1, 0, 3, 11}, []byte("example.com")...)
	request = append(request, 0x01, 0xbb)
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	reply = make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0 {
		t.Fatalf("connect reply=%v, want success", reply)
	}

	cancel()
	receiveWithin(t, stream.closeStarted, "active handler cleanup")
	requireNoReceive(t, errCh, "SOCKS server shutdown before active handler closed")

	stream.releaseClose()
	if err := receiveWithin(t, errCh, "SOCKS server shutdown"); err != nil {
		t.Fatalf("serveSOCKS returned %v", err)
	}
}

func TestHandleSOCKSConnCancelClosesActiveRelay(t *testing.T) {
	client, server := net.Pipe()
	defer func() {
		_ = client.Close()
	}()

	upstreamPeer, upstream := net.Pipe()
	defer func() {
		_ = upstreamPeer.Close()
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	opened := make(chan struct{}, 1)
	done := make(chan error, 1)
	cfg := Config{
		Upstream: testUpstream{
			openTCP: func(context.Context, string, int) (io.ReadWriteCloser, error) {
				opened <- struct{}{}
				return upstream, nil
			},
		},
	}
	go func() {
		done <- handleSOCKSConn(ctx, server, cfg)
	}()

	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{5, 0}) {
		t.Fatalf("handshake reply=%v, want [5 0]", reply)
	}
	request := append([]byte{5, 1, 0, 3, 11}, []byte("example.com")...)
	request = append(request, 0x01, 0xbb)
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	receiveWithin(t, opened, "upstream open")
	reply = make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0 {
		t.Fatalf("connect reply=%v, want success", reply)
	}

	cancel()
	if err := receiveWithin(t, done, "relay shutdown after cancel"); err != nil {
		t.Fatalf("handleSOCKSConn returned %v", err)
	}
}

func TestHandleSOCKSConnUDPAssociateRelaysDatagrams(t *testing.T) {
	client, server := net.Pipe()
	defer func() {
		_ = client.Close()
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	fakeUpstream := newFakeUDPStream()
	udpConn := newFakeUDPPacketConn("127.0.0.1:39000")
	opened := make(chan string, 1)
	logCh := make(chan string, 16)
	done := make(chan error, 1)
	cfg := Config{
		Logger: testLogger{log: func(_ string, format string, args ...any) {
			logCh <- fmt.Sprintf(format, args...)
		}},
		Upstream: testUpstream{
			openUDP: func(ctx context.Context, host string, port int) (datagram.Conn, error) {
				opened <- net.JoinHostPort(host, strconv.Itoa(port))
				return fakeUpstream, nil
			},
		},
		listenUDP: func(*net.UDPAddr) (udpPacketConn, error) {
			return udpConn, nil
		},
	}
	go func() {
		done <- handleSOCKSConn(ctx, server, cfg)
	}()

	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{5, 0}) {
		t.Fatalf("handshake reply=%v, want [5 0]", reply)
	}
	request := []byte{5, socksCommandUDPAssociate, 0, 1, 0, 0, 0, 0, 0, 0}
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	relayAddr := readUDPAssociateReply(t, client)
	if relayAddr.String() != udpConn.local.String() {
		t.Fatalf("relay addr=%s, want %s", relayAddr, udpConn.local)
	}

	ssPacket := []byte("query")
	packet, err := buildSOCKSUDPDatagram("example.com", 53, ssPacket)
	if err != nil {
		t.Fatal(err)
	}
	clientAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000}
	udpConn.reads <- fakeUDPDatagram{payload: packet, addr: clientAddr}

	target := receiveWithin(t, opened, "UDP upstream open")
	if target != "example.com:53" {
		t.Fatalf("opened target=%s, want example.com:53", target)
	}
	if got := receiveWithin(t, fakeUpstream.writes, "upstream write"); !bytes.Equal(got, ssPacket) {
		t.Fatalf("upstream payload=%x, want %x", got, ssPacket)
	}
	waitForLog(t, logCh, "[udp] open", "target=example.com:53")
	waitForLog(t, logCh, "update source=", "target=example.com:53")

	fakeUpstream.reads <- []byte("response")
	response := receiveWithin(t, udpConn.writes, "UDP response")
	if response.addr.String() != clientAddr.String() {
		t.Fatalf("response addr=%s, want %s", response.addr, clientAddr)
	}
	got, err := parseSOCKSUDPDatagram(response.payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "example.com" || got.Port != 53 || string(got.Payload) != "response" {
		t.Fatalf("response datagram=%+v", got)
	}

	cancel()
	if err := receiveWithin(t, done, "UDP associate shutdown"); err != nil {
		t.Fatalf("handleSOCKSConn returned %v", err)
	}
	receiveWithin(t, fakeUpstream.closed, "UDP target stream close")
}

func waitForLog(t *testing.T, logCh <-chan string, wants ...string) {
	t.Helper()
	receiveMatchingWithin(t, logCh, fmt.Sprintf("log containing %q", wants), func(msg string) bool {
		return logContainsAll(msg, wants...)
	})
}

func logContainsAll(msg string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			return false
		}
	}
	return true
}

type testLogger struct {
	log func(level string, format string, args ...any)
}

type testUpstream struct {
	openTCP func(context.Context, string, int) (io.ReadWriteCloser, error)
	openUDP func(context.Context, string, int) (datagram.Conn, error)
}

func (u testUpstream) OpenTCP(ctx context.Context, host string, port int) (io.ReadWriteCloser, error) {
	if u.openTCP == nil {
		return nil, fmt.Errorf("unexpected TCP open: %s", net.JoinHostPort(host, strconv.Itoa(port)))
	}
	return u.openTCP(ctx, host, port)
}

func (u testUpstream) OpenUDP(ctx context.Context, host string, port int) (datagram.Conn, error) {
	if u.openUDP == nil {
		return nil, fmt.Errorf("unexpected UDP open: %s", net.JoinHostPort(host, strconv.Itoa(port)))
	}
	return u.openUDP(ctx, host, port)
}

func (l testLogger) Infof(format string, args ...any) {
	l.write("info", format, args...)
}

func (l testLogger) Warnf(format string, args ...any) {
	l.write("warn", format, args...)
}

func (l testLogger) write(level string, format string, args ...any) {
	if l.log == nil {
		return
	}
	l.log(level, format, args...)
}

type pipeListener struct {
	conns     chan net.Conn
	errs      chan error
	closed    chan struct{}
	closeOnce sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		conns:  make(chan net.Conn, 1),
		errs:   make(chan error, 16),
		closed: make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	default:
	}
	select {
	case err := <-l.errs:
		return nil, err
	default:
	}
	select {
	case conn := <-l.conns:
		return conn, nil
	case err := <-l.errs:
		return nil, err
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

// failNextAccepts makes the next count Accept calls return err.
func (l *pipeListener) failNextAccepts(err error, count int) {
	for range count {
		l.errs <- err
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return testNetAddr("pipe")
}

func (l *pipeListener) Dial() (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		closeQuietly(client)
		closeQuietly(server)
		return nil, net.ErrClosed
	}
}

type testNetAddr string

func (a testNetAddr) Network() string {
	return "pipe"
}

func (a testNetAddr) String() string {
	return string(a)
}

func readUDPAssociateReply(t *testing.T, r io.Reader) *net.UDPAddr {
	t.Helper()
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		t.Fatal(err)
	}
	if header[0] != 5 || header[1] != 0 {
		t.Fatalf("UDP associate reply header=%v, want success", header)
	}
	var ip net.IP
	switch header[3] {
	case 1:
		raw := make([]byte, 4)
		if _, err := io.ReadFull(r, raw); err != nil {
			t.Fatal(err)
		}
		ip = net.IP(raw)
	case 4:
		raw := make([]byte, 16)
		if _, err := io.ReadFull(r, raw); err != nil {
			t.Fatal(err)
		}
		ip = net.IP(raw)
	default:
		t.Fatalf("unexpected reply address type: %d", header[3])
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		t.Fatal(err)
	}
	return &net.UDPAddr{IP: ip, Port: int(portBytes[0])<<8 | int(portBytes[1])}
}

type fakeUDPDatagram struct {
	payload []byte
	addr    *net.UDPAddr
}

type fakeUDPPacketConn struct {
	local     *net.UDPAddr
	reads     chan fakeUDPDatagram
	writes    chan fakeUDPDatagram
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeUDPPacketConn(local string) *fakeUDPPacketConn {
	addr, err := net.ResolveUDPAddr("udp", local)
	if err != nil {
		panic(err)
	}
	return &fakeUDPPacketConn{
		local:  addr,
		reads:  make(chan fakeUDPDatagram, 1),
		writes: make(chan fakeUDPDatagram, 1),
		closed: make(chan struct{}),
	}
}

func (c *fakeUDPPacketConn) LocalAddr() net.Addr {
	return c.local
}

func (c *fakeUDPPacketConn) ReadFromUDP(p []byte) (int, *net.UDPAddr, error) {
	select {
	case datagram := <-c.reads:
		return copy(p, datagram.payload), cloneUDPAddr(datagram.addr), nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	}
}

func (c *fakeUDPPacketConn) WriteToUDP(p []byte, addr *net.UDPAddr) (int, error) {
	datagram := fakeUDPDatagram{
		payload: append([]byte(nil), p...),
		addr:    cloneUDPAddr(addr),
	}
	select {
	case c.writes <- datagram:
		return len(p), nil
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *fakeUDPPacketConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
	})
	return nil
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	copied := *addr
	if addr.IP != nil {
		copied.IP = append(net.IP(nil), addr.IP...)
	}
	return &copied
}

type fakeUDPStream struct {
	reads  chan []byte
	writes chan []byte
	closed chan struct{}
}

func newFakeUDPStream() *fakeUDPStream {
	return &fakeUDPStream{
		reads:  make(chan []byte, 1),
		writes: make(chan []byte, 1),
		closed: make(chan struct{}),
	}
}

func (s *fakeUDPStream) ReadDatagram(p []byte) (int, error) {
	select {
	case payload := <-s.reads:
		return copy(p, payload), nil
	case <-s.closed:
		return 0, io.EOF
	}
}

func (s *fakeUDPStream) WriteDatagram(p []byte) (int, error) {
	payload := append([]byte(nil), p...)
	select {
	case s.writes <- payload:
		return len(p), nil
	case <-s.closed:
		return 0, io.ErrClosedPipe
	}
}

func (s *fakeUDPStream) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

type closeBlockingStream struct {
	closeStarted chan struct{}
	allowClose   chan struct{}
	readClosed   chan struct{}
	closeOnce    sync.Once
	releaseOnce  sync.Once
}

func newCloseBlockingStream() *closeBlockingStream {
	return &closeBlockingStream{
		closeStarted: make(chan struct{}),
		allowClose:   make(chan struct{}),
		readClosed:   make(chan struct{}),
	}
}

func (s *closeBlockingStream) Read([]byte) (int, error) {
	<-s.readClosed
	return 0, io.EOF
}

func (s *closeBlockingStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *closeBlockingStream) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeStarted)
		close(s.readClosed)
		<-s.allowClose
	})
	return nil
}

func (s *closeBlockingStream) releaseClose() {
	s.releaseOnce.Do(func() {
		close(s.allowClose)
	})
}

func (s *closeBlockingStream) ReadDatagram(p []byte) (int, error)  { return s.Read(p) }
func (s *closeBlockingStream) WriteDatagram(p []byte) (int, error) { return s.Write(p) }
