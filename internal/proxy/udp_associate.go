package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
)

const socksUDPRelayBufferSize = 64 * 1024

type udpPacketConn interface {
	io.Closer
	LocalAddr() net.Addr
	ReadFromUDP([]byte) (int, *net.UDPAddr, error)
	WriteToUDP([]byte, *net.UDPAddr) (int, error)
}

func handleSOCKSUDPAssociate(
	ctx context.Context,
	client net.Conn,
	reader *bufio.Reader,
	request socksRequest,
	cfg Config,
) error {
	udpConn, err := cfg.bindUDP(udpBindAddrForClient(client))
	if err != nil {
		cfg.warnf("[udp] bind relay socket failed source=%s err=%v", client.RemoteAddr(), err)
		writeSOCKSReplyQuietly(client, socksReplyConnectionRefused)
		return nil
	}

	if err := writeSOCKSReplyAddr(client, socksReplySucceeded, udpConn.LocalAddr()); err != nil {
		closeQuietly(udpConn)
		return err
	}

	tracker := newUDPConnectionTracker(cfg, client.RemoteAddr().String(), "UDP_ASSOCIATE "+request.Address)
	tracker.open()
	var relayErr error
	defer func() {
		tracker.close(relayErr)
	}()

	assocCtx, cancel := context.WithCancel(ctx)
	assoc := newUDPAssociation(assocCtx.Done(), cfg, udpConn, tracker)
	assoc.expectedClientAddr = expectedUDPClientAddr(client.RemoteAddr(), request.Port)
	// close waits for the idle sweeper, and the sweeper only returns once
	// assocCtx is done, so the cancel must run first. Deferring both here in one
	// function keeps that order a property of this block rather than something
	// relay has to guarantee by calling its stop func on every path.
	defer func() {
		cancel()
		assoc.close()
	}()

	if idleTimeout := cfg.udpTargetIdleTimeout(); idleTimeout > 0 {
		assoc.sweeperGroup.Go(func() {
			assoc.sweepIdleTargets(assocCtx, idleTimeout)
		})
	}

	relayErr = assoc.relay(assocCtx, cancel, client, reader)

	return nil
}

type udpAssociation struct {
	cfg     Config
	ctxDone <-chan struct{}
	udpConn udpPacketConn
	tracker *connectionTracker
	stats   *Stats

	// clientAddr is the pinned association client. It is written once, by the
	// first valid datagram, and read by every target relay goroutine, so it is
	// atomic and the pointed-to address is never mutated afterwards.
	clientAddr atomic.Pointer[net.UDPAddr]
	// expectedClientAddr ties UDP traffic back to the TCP control client. Its IP
	// always comes from the accepted TCP connection; a non-zero port comes from
	// the UDP_ASSOCIATE request, while zero leaves the source port to be pinned by
	// the first accepted datagram.
	expectedClientAddr *net.UDPAddr

	mu      sync.Mutex
	targets map[string]*udpTarget
	// openingTargets holds the destinations with an open in flight, along with
	// the datagrams waiting on each one, so only one open runs per destination
	// and datagrams that arrive during it are not lost. See
	// takeTargetForDatagram.
	openingTargets map[string]*openingTarget
	isClosed       bool

	// sniffedInnerTargets holds every sniffed inner destination already reported,
	// capped at maxSniffedInnerTargets. See recordInnerTarget.
	sniffedInnerTargets map[string]struct{}

	// warnedDrops records which drop reasons have already been warned about, so
	// a steady stream of dropped datagrams costs one log line instead of one per
	// datagram. See warnDropOnce.
	warnedDrops            map[string]struct{}
	warnedLabelOscillation bool
	label                  string

	targetGroup  sync.WaitGroup
	sweeperGroup sync.WaitGroup
	closeOnce    sync.Once
}

type udpClientPacket struct {
	addr     *net.UDPAddr
	datagram socksUDPDatagram
}

func newUDPAssociation(
	done <-chan struct{},
	cfg Config,
	udpConn udpPacketConn,
	tracker *connectionTracker,
) *udpAssociation {
	stats := &Stats{}
	if tracker != nil && tracker.stats != nil {
		stats = tracker.stats
	}
	return &udpAssociation{
		cfg:                 cfg,
		ctxDone:             done,
		udpConn:             udpConn,
		tracker:             tracker,
		targets:             make(map[string]*udpTarget),
		openingTargets:      make(map[string]*openingTarget),
		sniffedInnerTargets: make(map[string]struct{}),
		warnedDrops:         make(map[string]struct{}),
		stats:               stats,
	}
}

func (a *udpAssociation) relay(
	ctx context.Context,
	cancel context.CancelFunc,
	client net.Conn,
	reader *bufio.Reader,
) error {
	return runRelayPair(func() {
		cancel()
		closeQuietly(client)
		a.close()
	}, func() error {
		_, err := io.Copy(io.Discard, &bufferedConn{Conn: client, reader: reader})
		return err
	}, func() error {
		return a.relayFromClient(ctx)
	})
}

func (a *udpAssociation) relayFromClient(ctx context.Context) error {
	buf := make([]byte, socksUDPRelayBufferSize)
	for {
		packet, ok, err := a.readClientPacket(buf)
		if err != nil {
			if a.shouldStopRelay(err) {
				return nil
			}
			return err
		}
		if !ok {
			continue
		}
		if err := a.forwardClientPacket(ctx, packet); err != nil {
			if a.shouldStopRelay(err) {
				return nil
			}
			return err
		}
	}
}

func (a *udpAssociation) readClientPacket(buf []byte) (udpClientPacket, bool, error) {
	n, addr, err := a.udpConn.ReadFromUDP(buf)
	if err != nil {
		return udpClientPacket{}, false, err
	}
	if !a.allowsClientAddr(addr) {
		return udpClientPacket{}, false, nil
	}

	datagram, err := parseSOCKSUDPDatagram(buf[:n])
	if err != nil {
		if a.warnDropOnce(dropReasonMalformed) {
			a.cfg.warnf("[udp] drop malformed SOCKS5 datagram from=%s err=%v", addr, err)
		}
		return udpClientPacket{}, false, nil
	}
	if a.shouldDropClientDatagram(datagram) {
		return udpClientPacket{}, false, nil
	}
	return udpClientPacket{addr: addr, datagram: datagram}, true, nil
}

// Drop reasons are warned about once per association. Each one is a standing
// policy rather than a transient fault, so the condition repeats for every
// datagram a client keeps sending: a browser retrying QUIC produces UDP/443
// drops for as long as it is open, and a fake-IP or fragmenting client does the
// same. One line per datagram would then write the drop rate straight into the
// log file, which rotates but retains, so the first occurrence carries the
// diagnosis and the rest stay silent.
//
// Label oscillation is the same: when one association carries several inner
// targets through one Shadowsocks server, updateTarget fires per datagram and
// flips the label on nearly every one, logging and emitting an event each time.
// The first oscillation warns; the rest stay silent.
const (
	dropReasonMalformed      = "malformed"
	dropReasonFragmented     = "fragmented"
	dropReasonUDP443         = "x365-udp-443-disabled"
	dropReasonFakeIP         = "fake-ip"
	dropReasonForeign        = "not-associated-client"
	dropReasonTargetNotReady = "target-not-ready"
	dropReasonTargetLimit    = "target-limit"
)

func (a *udpAssociation) shouldDropClientDatagram(datagram socksUDPDatagram) bool {
	target := udpTargetKey(datagram.Host, datagram.Port)
	if datagram.Frag != 0 {
		if a.warnDropOnce(dropReasonFragmented) {
			a.cfg.warnf("[udp] drop fragmented SOCKS5 datagram frag=%d target=%s", datagram.Frag, target)
		}
		return true
	}
	if datagram.Port == 443 {
		if a.warnDropOnce(dropReasonUDP443) {
			a.cfg.warnf("[udp] drop target=%s reason=%s", target, dropReasonUDP443)
		}
		return true
	}
	if isFakeIPHost(datagram.Host) {
		if a.warnDropOnce(dropReasonFakeIP) {
			a.cfg.warnf("[udp] drop fake-ip target=%s", target)
		}
		return true
	}
	return false
}

// warnDropOnce reports whether reason should be logged, which is true only for
// the first dropped datagram with that reason on this association.
func (a *udpAssociation) warnDropOnce(reason string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.warnedDrops[reason]; ok {
		return false
	}
	a.warnedDrops[reason] = struct{}{}
	return true
}

func (a *udpAssociation) forwardClientPacket(ctx context.Context, packet udpClientPacket) error {
	datagram := packet.datagram
	if !a.adoptClientAddr(packet.addr) {
		return nil
	}
	key := udpTargetKey(datagram.Host, datagram.Port)
	// The open runs off this loop rather than on it, so a handshake never delays
	// datagrams bound for other destinations. Datagrams that arrive for a
	// destination still opening ride along with the open instead of being
	// dropped.
	target, dropReason, err := a.takeTargetForDatagram(ctx, key, datagram.Host, datagram.Port, datagram.Payload)
	if err != nil {
		if a.shouldStopRelay(err) {
			return err
		}
		return nil
	}
	if target != nil {
		a.sendToTarget(target, datagram.Payload)
		return nil
	}
	if dropReason != "" && a.warnDropOnce(dropReason) {
		a.cfg.warnf("[udp] drop target=%s reason=%s", key, dropReason)
	}
	return nil
}

func (a *udpAssociation) shouldStopRelay(err error) bool {
	return a.isContextDone() || errors.Is(err, net.ErrClosed)
}

func (a *udpAssociation) isContextDone() bool {
	select {
	case <-a.ctxDone:
		return true
	default:
		return false
	}
}

// adoptClientAddr pins the association to the first source that sends a valid
// datagram and reports whether addr is that source. RFC 1928 scopes a
// UDP_ASSOCIATE relay to the client that created it, so datagrams from any
// other local source are dropped instead of being forwarded; without this, the
// most recent sender would also capture every inbound response.
func (a *udpAssociation) adoptClientAddr(addr *net.UDPAddr) bool {
	if !a.allowsClientAddr(addr) {
		return false
	}
	// The steady state is an already-pinned client sending another datagram, so
	// that path compares without copying.
	pinned := a.clientAddr.Load()
	if pinned == nil {
		// ReadFromUDP reuses its buffer, so the address must be copied before
		// it is retained.
		copied := *addr
		copied.IP = slices.Clone(addr.IP)
		if a.clientAddr.CompareAndSwap(nil, &copied) {
			return true
		}
		// CAS failed, reload - this must succeed because another goroutine won
		pinned = a.clientAddr.Load()
		if pinned == nil {
			// Should never happen: another goroutine raced and won but we still
			// see nil. Reject defensively rather than panic in sameUDPAddr.
			return false
		}
	}
	if sameUDPAddr(pinned, addr) {
		return true
	}
	if a.warnDropOnce(dropReasonForeign) {
		a.cfg.warnf("[udp] drop datagram from=%s reason=%s client=%s", addr, dropReasonForeign, pinned)
	}
	return false
}

// allowsClientAddr rejects a source before parsing its SOCKS envelope. This is
// deliberately separate from pinning: only a valid, allowed datagram may claim
// an unspecified source port, but a foreign sender must not be able to produce
// malformed-packet logs before that happens.
func (a *udpAssociation) allowsClientAddr(addr *net.UDPAddr) bool {
	if !matchesExpectedUDPClient(a.expectedClientAddr, addr) {
		if a.warnDropOnce(dropReasonForeign) {
			a.cfg.warnf(
				"[udp] drop datagram from=%s reason=%s client=%s",
				addr,
				dropReasonForeign,
				formatExpectedUDPClient(a.expectedClientAddr),
			)
		}
		return false
	}
	pinned := a.clientAddr.Load()
	if pinned == nil || sameUDPAddr(pinned, addr) {
		return true
	}
	if a.warnDropOnce(dropReasonForeign) {
		a.cfg.warnf("[udp] drop datagram from=%s reason=%s client=%s", addr, dropReasonForeign, pinned)
	}
	return false
}

func expectedUDPClientAddr(remote net.Addr, requestedPort int) *net.UDPAddr {
	tcpAddr, ok := remote.(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		return nil
	}
	return &net.UDPAddr{
		IP:   slices.Clone(tcpAddr.IP),
		Port: requestedPort,
		Zone: tcpAddr.Zone,
	}
}

func matchesExpectedUDPClient(expected, actual *net.UDPAddr) bool {
	if expected == nil {
		return true
	}
	if actual == nil || !expected.IP.Equal(actual.IP) {
		return false
	}
	if expected.Zone != "" && expected.Zone != actual.Zone {
		return false
	}
	return expected.Port == 0 || expected.Port == actual.Port
}

func formatExpectedUDPClient(addr *net.UDPAddr) string {
	if addr == nil {
		return "unknown"
	}
	host := addr.IP.String()
	if addr.Zone != "" {
		host += "%" + addr.Zone
	}
	port := "*"
	if addr.Port != 0 {
		port = strconv.Itoa(addr.Port)
	}
	return net.JoinHostPort(host, port)
}

func sameUDPAddr(left, right *net.UDPAddr) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Port == right.Port && left.Zone == right.Zone && left.IP.Equal(right.IP)
}

// getClientAddr returns the pinned client address, or nil before one is pinned.
// The result is shared and must not be mutated; WriteToUDP only reads it.
func (a *udpAssociation) getClientAddr() *net.UDPAddr {
	return a.clientAddr.Load()
}

func udpBindAddrForClient(client net.Conn) *net.UDPAddr {
	ip := net.IPv4(127, 0, 0, 1)
	if tcpAddr, ok := client.LocalAddr().(*net.TCPAddr); ok && tcpAddr.IP != nil && !tcpAddr.IP.IsUnspecified() {
		ip = tcpAddr.IP
	}
	return &net.UDPAddr{IP: ip, Port: 0}
}
