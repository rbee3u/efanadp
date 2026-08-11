package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUDPAssociationCloseClosesTargetOnce(t *testing.T) {
	stream := newCloseCountingStream()
	assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})
	target := &udpTarget{
		key:  "example.com:53",
		host: "example.com",
		port: 53,
		conn: stream,
		responseHeader: mustSocksUDPHeader(
			t,
			"example.com",
			53,
		),
	}
	assoc.targets[target.key] = target
	assoc.targetGroup.Go(func() {
		assoc.relayFromTarget(target)
	})

	assoc.close()
	assoc.close()

	if got := stream.closeCount.Load(); got != 1 {
		t.Fatalf("target close count=%d, want 1", got)
	}
}

func TestUDPAssociationCloseClosesSocketAndUnblocksTargetRelay(t *testing.T) {
	udpConn := newCloseBlockingUDPPacketConn(t, "127.0.0.1:39000")
	stream := newOnePacketStream([]byte("response"))
	assoc := newUDPAssociation(t.Context().Done(), Config{}, udpConn, &connectionTracker{stats: &Stats{}})
	assoc.adoptClientAddr(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53000})
	target := &udpTarget{
		key:            udpTargetKey("example.com", 53),
		host:           "example.com",
		port:           53,
		conn:           stream,
		responseHeader: mustSocksUDPHeader(t, "example.com", 53),
	}
	assoc.targets[target.key] = target
	assoc.targetGroup.Go(func() {
		assoc.relayFromTarget(target)
	})

	receiveWithin(t, udpConn.writeStarted, "blocked UDP response write")
	done := make(chan struct{})
	go func() {
		assoc.close()
		close(done)
	}()
	receiveWithin(t, done, "association close")

	if got := udpConn.closeCount.Load(); got != 1 {
		t.Fatalf("UDP socket close count=%d, want 1", got)
	}
	if got := stream.closeCount.Load(); got != 1 {
		t.Fatalf("target stream close count=%d, want 1", got)
	}
}

func TestUDPAssociationGetTargetClosesDuplicateOpen(t *testing.T) {
	existing := newCloseCountingStream()
	duplicate := newCloseCountingStream()
	key := udpTargetKey("example.com", 53)
	var assoc *udpAssociation
	cfg := Config{
		Upstream: testUpstream{
			openUDP: func(_ context.Context, host string, port int) (io.ReadWriteCloser, error) {
				assoc.mu.Lock()
				assoc.targets[key] = &udpTarget{
					key:  key,
					host: host,
					port: port,
					conn: existing,
				}
				assoc.mu.Unlock()
				return duplicate, nil
			},
		},
	}
	assoc = newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})

	target, opened, err := assoc.getTarget(t.Context(), "example.com", 53)
	if err != nil {
		t.Fatal(err)
	}
	if opened {
		t.Fatal("target should be reused, not reported as newly opened")
	}
	if target == nil || target.conn != existing {
		t.Fatalf("target=%+v, want existing target", target)
	}
	if got := duplicate.closeCount.Load(); got != 1 {
		t.Fatalf("duplicate close count=%d, want 1", got)
	}
	if got := existing.closeCount.Load(); got != 0 {
		t.Fatalf("existing close count before association close=%d, want 0", got)
	}

	assoc.close()
	if got := existing.closeCount.Load(); got != 1 {
		t.Fatalf("existing close count after association close=%d, want 1", got)
	}
}

func TestUDPAssociationWriteFailureClosesTargetAndReopens(t *testing.T) {
	writeErr := errors.New("write failed")
	opened := make(chan *writeFailingStream, 2)
	cfg := Config{
		Upstream: testUpstream{
			openUDP: func(_ context.Context, host string, port int) (io.ReadWriteCloser, error) {
				if host != "example.com" || port != 53 {
					return nil, fmt.Errorf("unexpected target %s", udpTargetKey(host, port))
				}
				stream := newWriteFailingStream(writeErr)
				opened <- stream
				return stream, nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer assoc.close()

	packet := udpClientPacket{
		addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000},
		datagram: socksUDPDatagram{
			Host:    "example.com",
			Port:    53,
			Payload: []byte("query"),
		},
	}
	if err := assoc.forwardClientPacket(t.Context(), packet); err != nil {
		t.Fatal(err)
	}
	first := receiveWithin(t, opened, "first UDP target open")
	if got := first.closeCount.Load(); got != 1 {
		t.Fatalf("first target close count=%d, want 1", got)
	}
	if target, err := assoc.lookupTarget(udpTargetKey("example.com", 53)); err != nil || target != nil {
		t.Fatalf("target after write failure=%+v err=%v, want removed", target, err)
	}

	if err := assoc.forwardClientPacket(t.Context(), packet); err != nil {
		t.Fatal(err)
	}
	second := receiveWithin(t, opened, "second UDP target open")
	if second == first {
		t.Fatal("second datagram reused failed target")
	}
	if got := second.closeCount.Load(); got != 1 {
		t.Fatalf("second target close count=%d, want 1", got)
	}
}

func TestUDPAssociationDropsBlockedClientDatagrams(t *testing.T) {
	var logs []string
	cfg := Config{
		Logger: testLogger{log: func(_ string, format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})

	tests := []struct {
		name     string
		datagram socksUDPDatagram
		wantLog  string
	}{
		{
			name:     "fragmented",
			datagram: socksUDPDatagram{Frag: 1, Host: "example.com", Port: 53},
			wantLog:  "drop fragmented SOCKS5 datagram",
		},
		{
			name:     "udp 443",
			datagram: socksUDPDatagram{Host: "example.com", Port: 443},
			wantLog:  "reason=x365-udp-443-disabled",
		},
		{
			name:     "fake ip",
			datagram: socksUDPDatagram{Host: "198.18.0.1", Port: 53},
			wantLog:  "drop fake-ip",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs = nil
			if !assoc.shouldDropClientDatagram(tt.datagram) {
				t.Fatalf("shouldDropClientDatagram(%+v)=false, want true", tt.datagram)
			}
			if len(logs) != 1 || !strings.Contains(logs[0], tt.wantLog) {
				t.Fatalf("logs=%v, want one log containing %q", logs, tt.wantLog)
			}
		})
	}

	if assoc.shouldDropClientDatagram(socksUDPDatagram{Host: "example.com", Port: 53}) {
		t.Fatal("ordinary UDP datagram was dropped")
	}
}

func TestUDPAssociationDeduplicatesInnerTargetSniffUpdates(t *testing.T) {
	var logs []string
	events := make(chan Event, 4)
	cfg := Config{
		Logger: testLogger{log: func(_ string, format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}},
		OnConnection: func(event Event) {
			events <- event
		},
	}
	tracker := newUDPConnectionTracker(cfg, "127.0.0.1:50000", "example.com:53")
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, tracker)

	assoc.noteInnerTarget("example.com:53->inner.example:5353")
	assoc.noteInnerTarget("example.com:53->inner.example:5353")
	assoc.noteInnerTarget("example.com:53->other.example:5353")

	first := receiveWithin(t, events, "first target update")
	second := receiveWithin(t, events, "second target update")
	requireNoReceive(t, events, "duplicate target update")
	if first.Type != ConnectionUpdated || first.Target != "example.com:53->inner.example:5353" {
		t.Fatalf("first event=%+v, want first inner target update", first)
	}
	if second.Type != ConnectionUpdated || second.Target != "example.com:53->other.example:5353" {
		t.Fatalf("second event=%+v, want changed inner target update", second)
	}

	sniffLogs := 0
	for _, log := range logs {
		if strings.Contains(log, "[udp] sniff target=") {
			sniffLogs++
		}
	}
	if sniffLogs != 2 {
		t.Fatalf("sniff logs=%d (%v), want one per distinct inner target", sniffLogs, logs)
	}
}

func TestUDPAssociationPinsClientAndDropsForeignSources(t *testing.T) {
	var logs []string
	opens := make(chan string, 4)
	cfg := Config{
		Logger: testLogger{log: func(_ string, format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		}},
		Upstream: testUpstream{
			openUDP: func(_ context.Context, host string, port int) (io.ReadWriteCloser, error) {
				opens <- udpTargetKey(host, port)
				return newCloseCountingStream(), nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer assoc.close()

	client := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000}
	foreign := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 61000}
	datagram := socksUDPDatagram{Host: "example.com", Port: 53, Payload: []byte("query")}

	if err := assoc.forwardClientPacket(t.Context(), udpClientPacket{addr: client, datagram: datagram}); err != nil {
		t.Fatal(err)
	}
	if got := receiveWithin(t, opens, "client target open"); got != "example.com:53" {
		t.Fatalf("opened target=%s, want example.com:53", got)
	}

	// A second local process must not be relayed, and must not steal responses.
	if err := assoc.forwardClientPacket(t.Context(), udpClientPacket{addr: foreign, datagram: datagram}); err != nil {
		t.Fatal(err)
	}
	requireNoReceive(t, opens, "foreign source target open")
	if got := assoc.getClientAddr(); got.String() != client.String() {
		t.Fatalf("response addr=%s, want pinned client %s", got, client)
	}

	dropLogs := 0
	for _, log := range logs {
		if strings.Contains(log, "reason=not-associated-client") {
			dropLogs++
		}
	}
	if dropLogs != 1 {
		t.Fatalf("foreign-source drop logs=%d (%v), want exactly one", dropLogs, logs)
	}

	// Repeat datagrams from the pinned client keep flowing on the cached target.
	if err := assoc.forwardClientPacket(t.Context(), udpClientPacket{addr: client, datagram: datagram}); err != nil {
		t.Fatal(err)
	}
	requireNoReceive(t, opens, "reopen for pinned client")
}

func TestUDPAssociationSweepClosesIdleTargets(t *testing.T) {
	assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})
	idle := newCloseCountingStream()
	fresh := newCloseCountingStream()
	now := time.Now()

	idleTarget := &udpTarget{key: "idle:53", host: "idle", port: 53, conn: idle}
	idleTarget.touch(now.Add(-5 * time.Minute))
	freshTarget := &udpTarget{key: "fresh:53", host: "fresh", port: 53, conn: fresh}
	freshTarget.touch(now)
	assoc.targets[idleTarget.key] = idleTarget
	assoc.targets[freshTarget.key] = freshTarget

	taken := assoc.takeIdleTargets(now, time.Minute)
	if len(taken) != 1 || taken[0] != idleTarget {
		t.Fatalf("swept targets=%+v, want only the idle target", taken)
	}
	if _, ok := assoc.targets[idleTarget.key]; ok {
		t.Fatal("idle target still cached after sweep")
	}
	if _, ok := assoc.targets[freshTarget.key]; !ok {
		t.Fatal("active target was swept")
	}

	assoc.close()
	if got := fresh.closeCount.Load(); got != 1 {
		t.Fatalf("active target close count=%d, want 1 on association close", got)
	}
}

// A cached target must be marked active while the association lock is held, or
// the sweep can close it between the cache lookup and the write, which silently
// drops the datagram that reused it. Steady DNS traffic makes this the common
// case for a long-lived resolver target.
func TestUDPAssociationLookupKeepsTargetFromIdleSweep(t *testing.T) {
	assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})
	defer assoc.close()

	stream := newCloseCountingStream()
	key := udpTargetKey("resolver.example.net", 53)
	target := &udpTarget{key: key, host: "resolver.example.net", port: 53, conn: stream}
	stale := time.Now().Add(-5 * time.Minute)
	target.touch(stale)
	assoc.targets[key] = target

	found, err := assoc.lookupTarget(key)
	if err != nil || found != target {
		t.Fatalf("lookupTarget=%+v err=%v, want the cached target", found, err)
	}
	if idle := target.idleSince(time.Now()); idle > time.Second {
		t.Fatalf("target idle=%s after a cache hit, want it marked active", idle)
	}

	if swept := assoc.takeIdleTargets(time.Now(), time.Minute); len(swept) != 0 {
		t.Fatalf("swept %d targets, want the just-used target kept", len(swept))
	}
	if _, ok := assoc.targets[key]; !ok {
		t.Fatal("target in active use was swept")
	}
}

// openUDP defers its X365 response read to the first target read, so a rejected
// upstream only surfaces here. Without a log the client just sees no reply.
func TestUDPAssociationLogsTargetReadFailure(t *testing.T) {
	var logs []string
	readErr := errors.New("bad X365 response: deadbeef")
	cfg := Config{Logger: testLogger{log: func(_ string, format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	target := &udpTarget{
		key:            udpTargetKey("example.org", 53),
		host:           "example.org",
		port:           53,
		conn:           &readFailingStream{err: readErr},
		responseHeader: mustSocksUDPHeader(t, "example.org", 53),
	}
	assoc.targets[target.key] = target

	assoc.relayFromTarget(target)

	if _, ok := assoc.targets[target.key]; ok {
		t.Fatal("target still cached after a read failure")
	}
	found := slices.ContainsFunc(logs, func(log string) bool {
		return strings.Contains(log, "[udp] read response") && strings.Contains(log, readErr.Error())
	})
	if !found {
		t.Fatalf("logs=%v, want a read-response warning naming the upstream error", logs)
	}
}

// Close, context teardown, and the idle sweep are all normal, so they must not
// log an upstream failure.
func TestUDPAssociationDoesNotLogBenignTargetReadErrors(t *testing.T) {
	var logs []string
	cfg := Config{Logger: testLogger{log: func(_ string, format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}}}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	for _, err := range []error{net.ErrClosed, io.EOF} {
		target := &udpTarget{
			key:            udpTargetKey("example.net", 5353),
			host:           "example.net",
			port:           5353,
			conn:           &readFailingStream{err: err},
			responseHeader: mustSocksUDPHeader(t, "example.net", 5353),
		}
		assoc.targets[target.key] = target
		assoc.relayFromTarget(target)
	}
	if len(logs) != 0 {
		t.Fatalf("logs=%v, want no warning for benign read errors", logs)
	}
}

type readFailingStream struct {
	err error
}

func (s *readFailingStream) Read([]byte) (int, error) { return 0, s.err }
func (s *readFailingStream) Write(p []byte) (int, error) {
	return len(p), nil
}
func (s *readFailingStream) Close() error { return nil }

func TestUDPAssociationSweepStopsWithContext(t *testing.T) {
	assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		assoc.sweepIdleTargets(ctx, time.Minute)
		close(done)
	}()
	cancel()
	receiveWithin(t, done, "idle sweeper shutdown")
}

func mustSocksUDPHeader(tb testing.TB, host string, port int) []byte {
	tb.Helper()
	header, err := buildSocksUDPHeader(host, port)
	if err != nil {
		tb.Fatal(err)
	}
	return header
}

type closeBlockingUDPPacketConn struct {
	local        *net.UDPAddr
	closed       chan struct{}
	writeStarted chan struct{}
	writeOnce    sync.Once
	closeOnce    sync.Once
	closeCount   atomic.Int32
}

func newCloseBlockingUDPPacketConn(tb testing.TB, addr string) *closeBlockingUDPPacketConn {
	tb.Helper()
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		tb.Fatal(err)
	}
	return &closeBlockingUDPPacketConn{
		local:        udpAddr,
		closed:       make(chan struct{}),
		writeStarted: make(chan struct{}),
	}
}

func (c *closeBlockingUDPPacketConn) LocalAddr() net.Addr {
	return c.local
}

func (c *closeBlockingUDPPacketConn) ReadFromUDP([]byte) (int, *net.UDPAddr, error) {
	<-c.closed
	return 0, nil, net.ErrClosed
}

func (c *closeBlockingUDPPacketConn) WriteToUDP(p []byte, _ *net.UDPAddr) (int, error) {
	c.writeOnce.Do(func() {
		close(c.writeStarted)
	})
	<-c.closed
	return 0, net.ErrClosed
}

func (c *closeBlockingUDPPacketConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeCount.Add(1)
		close(c.closed)
	})
	return nil
}

type closeCountingStream struct {
	closed     chan struct{}
	closeCount atomic.Int32
}

func newCloseCountingStream() *closeCountingStream {
	return &closeCountingStream{closed: make(chan struct{})}
}

func (s *closeCountingStream) Read([]byte) (int, error) {
	<-s.closed
	return 0, net.ErrClosed
}

func (s *closeCountingStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *closeCountingStream) Close() error {
	if s.closeCount.Add(1) == 1 {
		close(s.closed)
	}
	return nil
}

type writeFailingStream struct {
	*closeCountingStream
	err error
}

func newWriteFailingStream(err error) *writeFailingStream {
	return &writeFailingStream{
		closeCountingStream: newCloseCountingStream(),
		err:                 err,
	}
}

func (s *writeFailingStream) Write([]byte) (int, error) {
	return 0, s.err
}

type onePacketStream struct {
	payload    []byte
	closed     chan struct{}
	sent       atomic.Bool
	closeCount atomic.Int32
}

func newOnePacketStream(payload []byte) *onePacketStream {
	return &onePacketStream{
		payload: append([]byte(nil), payload...),
		closed:  make(chan struct{}),
	}
}

func (s *onePacketStream) Read(p []byte) (int, error) {
	if !s.sent.Swap(true) {
		return copy(p, s.payload), nil
	}
	<-s.closed
	return 0, net.ErrClosed
}

func (s *onePacketStream) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *onePacketStream) Close() error {
	if s.closeCount.Add(1) == 1 {
		close(s.closed)
	}
	return nil
}
