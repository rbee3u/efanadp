package proxy

import (
	"bytes"
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

	"github.com/rbee3u/efanadp/internal/x365"
)

func TestUDPAssociationCloseClosesTargetOnce(t *testing.T) {
	stream := newCloseCountingStream()
	assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})
	target := &udpTarget{
		key:  "example.com:53",
		host: "example.com",
		port: 53,
		conn: stream,
		responseHeader: mustSOCKSUDPHeader(
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
		responseHeader: mustSOCKSUDPHeader(t, "example.com", 53),
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

	assoc.openTargetAndDrain(t.Context(), key, "example.com", 53)

	target, err := assoc.lookupTarget(key)
	if err != nil {
		t.Fatal(err)
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
	waitFor(t, func() bool {
		return first.closeCount.Load() == 1
	}, "first target close after write failure")
	waitFor(t, func() bool {
		target, err := assoc.lookupTarget(udpTargetKey("example.com", 53))
		return err == nil && target == nil
	}, "failed target removal")

	if err := assoc.forwardClientPacket(t.Context(), packet); err != nil {
		t.Fatal(err)
	}
	second := receiveWithin(t, opened, "second UDP target open")
	if second == first {
		t.Fatal("second datagram reused failed target")
	}
	waitFor(t, func() bool {
		return second.closeCount.Load() == 1
	}, "second target close after write failure")
}

// Opening a destination dials TCP, completes a REALITY handshake, and writes the
// X365 request. The client relay loop is the association's only reader, so an
// open running on it delays every other destination behind a full handshake and
// drops datagrams once the receive buffer fills.
func TestUDPAssociationSlowTargetOpenDoesNotBlockOtherTargets(t *testing.T) {
	release := make(chan struct{})
	fast := newWriteRecordingStream()
	slowDialing := make(chan struct{}, 1)
	cfg := Config{
		Upstream: testUpstream{
			openUDP: func(_ context.Context, host string, _ int) (io.ReadWriteCloser, error) {
				if host == "slow.example.net" {
					slowDialing <- struct{}{}
					<-release
					return newCloseCountingStream(), nil
				}
				return fast, nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer func() {
		close(release)
		assoc.close()
	}()

	client := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000}
	forward := func(host string, payload string) {
		t.Helper()
		packet := udpClientPacket{
			addr: client,
			datagram: socksUDPDatagram{
				Host:    host,
				Port:    53,
				Payload: []byte(payload),
			},
		}
		if err := assoc.forwardClientPacket(t.Context(), packet); err != nil {
			t.Fatal(err)
		}
	}

	// A destination whose open never completes must not hold the relay loop.
	forward("slow.example.net", "slow query")
	receiveWithin(t, slowDialing, "slow target dial")

	forward("fast.example.net", "fast query")
	if got := receiveWithin(t, fast.writes, "fast target write"); string(got) != "fast query" {
		t.Fatalf("fast target payload=%q, want %q", got, "fast query")
	}
}

// Only one open runs per destination. Without that guard every datagram arriving
// during an open starts another dial for the same target, and all but one are
// closed again as duplicates.
func TestUDPAssociationOpensEachTargetOnceWhileOpening(t *testing.T) {
	release := make(chan struct{})
	var dials atomic.Int32
	dialing := make(chan struct{}, 1)
	var logs logRecorder
	cfg := Config{
		Logger: logs.logger(),
		Upstream: testUpstream{
			openUDP: func(_ context.Context, _ string, _ int) (io.ReadWriteCloser, error) {
				if dials.Add(1) == 1 {
					dialing <- struct{}{}
				}
				<-release
				return newCloseCountingStream(), nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer func() {
		close(release)
		assoc.close()
	}()

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
	receiveWithin(t, dialing, "first target dial")

	for range 5 {
		if err := assoc.forwardClientPacket(t.Context(), packet); err != nil {
			t.Fatal(err)
		}
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials=%d while one open is in flight, want 1", got)
	}

	// The five repeats fit the pending queue, so none of them is dropped as
	// target-not-ready. TestUDPAssociationQueuesDatagramsWhileTargetOpens covers
	// that they are also delivered in order once the open completes.
	if got := logs.count("reason=target-not-ready"); got != 0 {
		t.Fatalf("target-not-ready warnings=%d while queue has capacity, want 0", got)
	}
}

// Datagrams arriving while an open is in flight queue onto it rather than being
// dropped, so a stub resolver's A and AAAA queries sent together both reach the
// destination on the first lookup.
func TestUDPAssociationQueuesDatagramsWhileTargetOpens(t *testing.T) {
	release := make(chan struct{})
	dialing := make(chan struct{}, 1)
	stream := newWriteRecordingStream()
	cfg := Config{
		Logger: testLogger{},
		Upstream: testUpstream{
			openUDP: func(_ context.Context, _ string, _ int) (io.ReadWriteCloser, error) {
				dialing <- struct{}{}
				<-release
				return stream, nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer assoc.close()

	packet := func(payload string) udpClientPacket {
		return udpClientPacket{
			addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000},
			datagram: socksUDPDatagram{
				Host:    "example.com",
				Port:    53,
				Payload: []byte(payload),
			},
		}
	}

	// Trigger an open and queue 7 more datagrams (8 total fits in the queue).
	if err := assoc.forwardClientPacket(t.Context(), packet("first")); err != nil {
		t.Fatal(err)
	}
	receiveWithin(t, dialing, "target dial")

	for i := 2; i <= 8; i++ {
		if err := assoc.forwardClientPacket(t.Context(), packet(fmt.Sprintf("q%d", i))); err != nil {
			t.Fatal(err)
		}
	}

	// Let the open complete and verify all queued datagrams were sent.
	close(release)
	var received []string
	for i := 1; i <= 8; i++ {
		received = append(received, string(receiveWithin(t, stream.writes, fmt.Sprintf("write %d", i))))
	}
	requireNoReceive(t, stream.writes, "extra write")

	want := []string{"first", "q2", "q3", "q4", "q5", "q6", "q7", "q8"}
	if !slices.Equal(received, want) {
		t.Fatalf("received=%v, want %v", received, want)
	}
}

// A cache miss starts a TCP dial and REALITY handshake in the background, so a
// stream of unique destinations must not create an unbounded number of pending
// handshakes before any of them finishes. Open targets count against the same
// bound because each retains a stream, relay goroutine, and read buffer.
func TestUDPAssociationBoundsDistinctTargets(t *testing.T) {
	release := make(chan struct{})
	var dials atomic.Int32
	var logs logRecorder
	cfg := Config{
		Logger: logs.logger(),
		Upstream: testUpstream{
			openUDP: func(_ context.Context, _ string, _ int) (io.ReadWriteCloser, error) {
				dials.Add(1)
				<-release
				return newCloseCountingStream(), nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer func() {
		close(release)
		assoc.close()
	}()

	client := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000}
	for i := range maxUDPTargetsPerAssociation + 10 {
		packet := udpClientPacket{
			addr: client,
			datagram: socksUDPDatagram{
				Host:    fmt.Sprintf("target-%d.example.net", i),
				Port:    53,
				Payload: []byte("query"),
			},
		}
		if err := assoc.forwardClientPacket(t.Context(), packet); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(t, func() bool {
		return dials.Load() == maxUDPTargetsPerAssociation
	}, "bounded UDP target dials")
	assoc.mu.Lock()
	opening := len(assoc.openingTargets)
	assoc.mu.Unlock()
	if opening != maxUDPTargetsPerAssociation {
		t.Fatalf("opening targets=%d, want %d", opening, maxUDPTargetsPerAssociation)
	}
	if got := logs.count("reason=target-limit"); got != 1 {
		t.Fatalf("target-limit warnings=%d (%v), want exactly one", got, logs.all())
	}
}

// Each drop reason is a standing policy, not a transient fault, so the condition
// repeats for every datagram the client keeps sending. The datagram must be
// dropped every time while the reason is named exactly once: logging each repeat
// would write the drop rate into the log file, and rotation retains rather than
// discards. A browser retrying QUIC is the realistic source.
func TestUDPAssociationDropsBlockedDatagramsAndWarnsOncePerReason(t *testing.T) {
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
			var logs logRecorder
			assoc := newUDPAssociation(
				t.Context().Done(),
				Config{Logger: logs.logger()},
				nil,
				&connectionTracker{stats: &Stats{}},
			)

			for i := range 5 {
				if !assoc.shouldDropClientDatagram(tt.datagram) {
					t.Fatalf("datagram #%d was forwarded, want dropped every time", i)
				}
			}
			if got := logs.all(); len(got) != 1 || !strings.Contains(got[0], tt.wantLog) {
				t.Fatalf("logs=%v, want exactly one containing %q", got, tt.wantLog)
			}
		})
	}

	t.Run("ordinary datagram", func(t *testing.T) {
		assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})
		if assoc.shouldDropClientDatagram(socksUDPDatagram{Host: "example.com", Port: 53}) {
			t.Fatal("ordinary UDP datagram was dropped")
		}
	})
}

func TestUDPAssociationDropsMalformedDatagramsAndWarnsOnce(t *testing.T) {
	var logs logRecorder
	udpConn := newFakeUDPPacketConn("127.0.0.1:39000")
	assoc := newUDPAssociation(
		t.Context().Done(),
		Config{Logger: logs.logger()},
		udpConn,
		&connectionTracker{stats: &Stats{}},
	)
	defer assoc.close()

	client := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53000}
	for range 5 {
		udpConn.reads <- fakeUDPDatagram{payload: []byte{0xff}, addr: client}
		if _, ok, err := assoc.readClientPacket(make([]byte, 64)); err != nil || ok {
			t.Fatalf("malformed packet ok=%v err=%v, want a quiet drop", ok, err)
		}
	}
	if got := logs.all(); len(got) != 1 || !strings.Contains(got[0], "drop malformed SOCKS5 datagram") {
		t.Fatalf("logs=%v, want one malformed-packet warning", got)
	}
}

func TestUDPAssociationDeduplicatesInnerTargetSniffUpdates(t *testing.T) {
	var logs logRecorder
	events := make(chan Event, 4)
	cfg := Config{
		Logger: logs.logger(),
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

	if got := logs.count("[udp] sniff target="); got != 2 {
		t.Fatalf("sniff logs=%d (%v), want one per distinct inner target", got, logs.all())
	}
}

// One association can carry many inner destinations through a single
// Shadowsocks server. The label holds one at a time, so a client alternating
// between peers flips it on nearly every datagram; the warning about that must
// not repeat per flip, and the chain cap must bound the tracking map. The first
// chain is reported but does not warn as oscillation because the label started
// as the outer target, not another chain.
func TestUDPAssociationWarnsOnceOnInnerTargetOscillation(t *testing.T) {
	var logs logRecorder
	cfg := Config{Logger: logs.logger()}
	tracker := newUDPConnectionTracker(cfg, "127.0.0.1:50000", "203.0.113.10:39281")
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, tracker)

	// Real flow: a destination opens, then datagrams sniff chains.
	assoc.noteTargetOpened("203.0.113.10:39281")
	for i := range 12 {
		assoc.noteInnerTarget(fmt.Sprintf("203.0.113.10:39281->peer%d.example:5353", i))
	}

	if got := logs.count("label oscillation"); got != 1 {
		t.Fatalf("oscillation warnings=%d (%v), want exactly one", got, logs.all())
	}

	// Distinct chains beyond the cap stop being tracked, so the map cannot grow
	// without bound behind a server relaying for many peers.
	for i := range maxSniffedInnerTargets * 2 {
		assoc.noteInnerTarget(fmt.Sprintf("203.0.113.10:39281->flood%d.example:5353", i))
	}
	assoc.mu.Lock()
	tracked := len(assoc.sniffedInnerTargets)
	assoc.mu.Unlock()
	if tracked > maxSniffedInnerTargets {
		t.Fatalf("tracked inner targets=%d, want at most %d", tracked, maxSniffedInnerTargets)
	}
}

// The label starts as the outer destination, so the first sniffed chain replaces
// it as a matter of course. Only a move between two chains is oscillation, and
// reporting the first discovery as one would warn on every association that
// sniffs anything.
func TestUDPAssociationDoesNotWarnOnFirstInnerTarget(t *testing.T) {
	var logs logRecorder
	cfg := Config{Logger: logs.logger()}
	tracker := newUDPConnectionTracker(cfg, "127.0.0.1:50000", "203.0.113.10:39281")
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, tracker)

	assoc.noteTargetOpened("203.0.113.10:39281")
	assoc.noteInnerTarget("203.0.113.10:39281->a.example:5353")

	if got := logs.count("label oscillation"); got != 0 {
		t.Fatalf("warned on first inner target: %v", logs.all())
	}
}

// An association pins the first source it sees. When several local sources send
// their first datagram at once, exactly one must win and the pinned address must
// be that winner's: a partial adoption would leave responses addressed to a
// source that was never accepted.
func TestUDPAssociationAdoptClientAddrPinsOneWinnerUnderRace(t *testing.T) {
	const sources = 10
	assoc := newUDPAssociation(t.Context().Done(), Config{}, nil, &connectionTracker{stats: &Stats{}})

	addrs := make([]*net.UDPAddr, sources)
	for i := range sources {
		addrs[i] = &net.UDPAddr{IP: net.IPv4(127, 0, 0, byte(i+1)), Port: 50000 + i}
	}

	adopted := make([]bool, sources)
	var wg sync.WaitGroup
	for i := range sources {
		wg.Go(func() {
			adopted[i] = assoc.adoptClientAddr(addrs[i])
		})
	}
	wg.Wait()

	winners := 0
	var winner *net.UDPAddr
	for i, won := range adopted {
		if won {
			winners++
			winner = addrs[i]
		}
	}
	if winners != 1 {
		t.Fatalf("adopted count=%d, want exactly 1 winner", winners)
	}
	if pinned := assoc.clientAddr.Load(); pinned == nil || !sameUDPAddr(pinned, winner) {
		t.Fatalf("pinned addr=%v, want the winning source %v", pinned, winner)
	}
}

// UDP_ASSOCIATE belongs to the peer that owns its TCP control connection. A
// datagram from another IP must not be allowed to win the first-source race, and
// a non-zero source port declared in the request narrows the endpoint further.
func TestUDPAssociationRejectsUnexpectedClientBeforePinning(t *testing.T) {
	var logs logRecorder
	assoc := newUDPAssociation(
		t.Context().Done(),
		Config{Logger: logs.logger()},
		nil,
		&connectionTracker{stats: &Stats{}},
	)
	assoc.expectedClientAddr = &net.UDPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 53000}

	wrongIP := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 11), Port: 53000}
	wrongPort := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 53001}
	client := &net.UDPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 53000}
	if assoc.adoptClientAddr(wrongIP) {
		t.Fatal("datagram from a different IP claimed the association")
	}
	if assoc.adoptClientAddr(wrongPort) {
		t.Fatal("datagram from a different declared port claimed the association")
	}
	if pinned := assoc.getClientAddr(); pinned != nil {
		t.Fatalf("unexpected source was pinned as %s", pinned)
	}
	if !assoc.adoptClientAddr(client) {
		t.Fatal("TCP control client's declared UDP endpoint was rejected")
	}
	if pinned := assoc.getClientAddr(); !sameUDPAddr(pinned, client) {
		t.Fatalf("pinned addr=%v, want %v", pinned, client)
	}
	if got := logs.count("reason=not-associated-client"); got != 1 {
		t.Fatalf("foreign-source warnings=%d (%v), want one", got, logs.all())
	}
}

func TestUDPAssociationRejectsForeignSourceBeforeParsing(t *testing.T) {
	var logs logRecorder
	udpConn := newFakeUDPPacketConn("127.0.0.1:39000")
	assoc := newUDPAssociation(
		t.Context().Done(),
		Config{Logger: logs.logger()},
		udpConn,
		&connectionTracker{stats: &Stats{}},
	)
	defer assoc.close()
	assoc.expectedClientAddr = &net.UDPAddr{IP: net.IPv4(203, 0, 113, 10)}
	udpConn.reads <- fakeUDPDatagram{
		payload: []byte{0xff},
		addr:    &net.UDPAddr{IP: net.IPv4(203, 0, 113, 11), Port: 53000},
	}

	if _, ok, err := assoc.readClientPacket(make([]byte, 64)); err != nil || ok {
		t.Fatalf("foreign packet ok=%v err=%v, want a quiet drop", ok, err)
	}
	if got := logs.all(); len(got) != 1 || !strings.Contains(got[0], "reason=not-associated-client") {
		t.Fatalf("logs=%v, want only the foreign-source warning", got)
	}
}

func TestExpectedUDPClientAllowsUnspecifiedRequestPort(t *testing.T) {
	remote := &net.TCPAddr{IP: net.ParseIP("2001:db8::10"), Port: 41000, Zone: "test-zone"}
	expected := expectedUDPClientAddr(remote, 0)

	if expected == nil || expected.Port != 0 || !expected.IP.Equal(remote.IP) || expected.Zone != remote.Zone {
		t.Fatalf("expected endpoint=%v, want TCP IP and an unspecified port", expected)
	}
	if !matchesExpectedUDPClient(expected, &net.UDPAddr{
		IP:   net.ParseIP("2001:db8::10"),
		Port: 53000,
		Zone: "test-zone",
	}) {
		t.Fatal("unspecified request port did not accept the TCP peer's UDP source")
	}
	if matchesExpectedUDPClient(expected, &net.UDPAddr{
		IP:   net.ParseIP("2001:db8::11"),
		Port: 53000,
		Zone: "test-zone",
	}) {
		t.Fatal("unspecified request port also relaxed the TCP peer IP")
	}
}

func TestUDPAssociationPinsClientAndDropsForeignSources(t *testing.T) {
	var logs logRecorder
	opens := make(chan string, 4)
	cfg := Config{
		Logger: logs.logger(),
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
	waitFor(t, func() bool {
		target, err := assoc.lookupTarget(udpTargetKey("example.com", 53))
		return err == nil && target != nil
	}, "client target registration")

	// A second local process must not be relayed, and must not steal responses.
	// Being foreign is a standing condition, so a client that keeps sending is
	// dropped every time but named in the log only once.
	for range 5 {
		if err := assoc.forwardClientPacket(t.Context(), udpClientPacket{addr: foreign, datagram: datagram}); err != nil {
			t.Fatal(err)
		}
	}
	requireNoReceive(t, opens, "foreign source target open")
	if got := assoc.getClientAddr(); got.String() != client.String() {
		t.Fatalf("response addr=%s, want pinned client %s", got, client)
	}

	if got := logs.count("reason=not-associated-client"); got != 1 {
		t.Fatalf("foreign-source drop logs=%d (%v), want exactly one", got, logs.all())
	}

	// Repeat datagrams from the pinned client keep flowing on the cached target.
	if err := assoc.forwardClientPacket(t.Context(), udpClientPacket{addr: client, datagram: datagram}); err != nil {
		t.Fatal(err)
	}
	requireNoReceive(t, opens, "reopen for pinned client")
}

// The Shadowsocks UDP sniffer is derived once per target instead of once per
// datagram, so a target opened for a configured server must carry one and a
// target opened for anything else must not. Sniff correctness itself is covered
// in internal/x365.
func TestUDPTargetCarriesShadowsocksSnifferForConfiguredServer(t *testing.T) {
	const (
		ssHost = "203.0.113.10"
		ssPort = 39281
	)
	sniffConfigs, err := x365.NormalizeShadowsocksSniffConfigs([]x365.ShadowsocksSniffConfig{{
		Server:   ssHost,
		Port:     ssPort,
		Method:   "aes-256-gcm",
		Password: "secret",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ShadowsocksSniffConfigs: sniffConfigs,
		Upstream: testUpstream{
			openUDP: func(_ context.Context, _ string, _ int) (io.ReadWriteCloser, error) {
				return newCloseCountingStream(), nil
			},
		},
	}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	defer assoc.close()

	tests := []struct {
		name        string
		host        string
		port        int
		wantSniffer bool
	}{
		{name: "configured server", host: ssHost, port: ssPort, wantSniffer: true},
		{name: "other port", host: ssHost, port: 53, wantSniffer: false},
		{name: "other host", host: "203.0.113.11", port: ssPort, wantSniffer: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := assoc.openTarget(t.Context(), udpTargetKey(tt.host, tt.port), tt.host, tt.port)
			if err != nil {
				t.Fatal(err)
			}
			defer closeQuietly(target.conn)
			if gotSniffer := target.sniffer != nil; gotSniffer != tt.wantSniffer {
				t.Fatalf("target %s has sniffer=%v, want %v", target.key, gotSniffer, tt.wantSniffer)
			}
		})
	}
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
	var logs logRecorder
	readErr := errors.New("bad X365 response: deadbeef")
	cfg := Config{Logger: logs.logger()}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	target := &udpTarget{
		key:            udpTargetKey("example.org", 53),
		host:           "example.org",
		port:           53,
		conn:           &readFailingStream{err: readErr},
		responseHeader: mustSOCKSUDPHeader(t, "example.org", 53),
	}
	assoc.targets[target.key] = target

	assoc.relayFromTarget(target)

	if _, ok := assoc.targets[target.key]; ok {
		t.Fatal("target still cached after a read failure")
	}
	found := slices.ContainsFunc(logs.all(), func(log string) bool {
		return strings.Contains(log, "[udp] read response") && strings.Contains(log, readErr.Error())
	})
	if !found {
		t.Fatalf("logs=%v, want a read-response warning naming the upstream error", logs.all())
	}
}

// Close, context teardown, and the idle sweep are all normal, so they must not
// log an upstream failure.
func TestUDPAssociationDoesNotLogBenignTargetReadErrors(t *testing.T) {
	var logs logRecorder
	cfg := Config{Logger: logs.logger()}
	assoc := newUDPAssociation(t.Context().Done(), cfg, nil, &connectionTracker{stats: &Stats{}})
	for _, err := range []error{net.ErrClosed, io.EOF} {
		target := &udpTarget{
			key:            udpTargetKey("example.net", 5353),
			host:           "example.net",
			port:           5353,
			conn:           &readFailingStream{err: err},
			responseHeader: mustSOCKSUDPHeader(t, "example.net", 5353),
		}
		assoc.targets[target.key] = target
		assoc.relayFromTarget(target)
	}
	if got := logs.all(); len(got) != 0 {
		t.Fatalf("logs=%v, want no warning for benign read errors", got)
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

func mustSOCKSUDPHeader(tb testing.TB, host string, port int) []byte {
	tb.Helper()
	header, err := buildSOCKSUDPHeader(host, port)
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

// writeRecordingStream reports each payload written to it, so a test can observe
// a datagram reaching its destination.
type writeRecordingStream struct {
	*closeCountingStream
	writes chan []byte
}

func newWriteRecordingStream() *writeRecordingStream {
	return &writeRecordingStream{
		closeCountingStream: newCloseCountingStream(),
		writes:              make(chan []byte, 8),
	}
}

func (s *writeRecordingStream) Write(p []byte) (int, error) {
	select {
	case s.writes <- bytes.Clone(p):
	default:
	}
	return len(p), nil
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
