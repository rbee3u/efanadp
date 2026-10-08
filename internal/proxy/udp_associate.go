package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
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

	tracker := newConnectionTracker(cfg, "udp", client.RemoteAddr().String(), "UDP_ASSOCIATE "+request.address())
	tracker.open()
	var relayErr error
	defer func() {
		tracker.close(relayErr)
	}()

	assocCtx, cancel := context.WithCancel(ctx)
	assoc := newUDPAssociation(assocCtx.Done(), cfg, udpConn, tracker)
	assoc.expectedClientAddr = expectedUDPClientAddr(client.RemoteAddr(), request.Port)
	// Cancel before close: close waits for the sweeper, which exits on
	// cancellation.
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

	// clientAddr is pinned once and shared by target relays; its value is
	// immutable.
	clientAddr atomic.Pointer[net.UDPAddr]
	// The TCP peer supplies the IP; a non-zero request port constrains the
	// source. Port zero is pinned by the first accepted datagram.
	expectedClientAddr *net.UDPAddr

	mu      sync.Mutex
	targets map[string]*udpTarget
	// Opening targets retain queued datagrams until drained; see
	// takeTargetForDatagram.
	openingTargets map[string]*openingTarget
	isClosed       bool

	observation udpObservation // guarded by mu

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
		cfg:            cfg,
		ctxDone:        done,
		udpConn:        udpConn,
		tracker:        tracker,
		targets:        make(map[string]*udpTarget),
		openingTargets: make(map[string]*openingTarget),
		observation:    newUDPObservation(),
		stats:          stats,
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

// Warn once per reason per association to avoid per-packet log volume.
const (
	dropReasonMalformed      = "malformed"
	dropReasonFragmented     = "fragmented"
	dropReasonUDP443         = "x365-udp-443-disabled"
	dropReasonFakeIP         = "fake-ip"
	dropReasonForeign        = "not-associated-client"
	dropReasonTargetNotReady = "target-not-ready"
	dropReasonTargetLimit    = "target-limit"
	dropReasonClientWrite    = "client-write"
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

func (a *udpAssociation) forwardClientPacket(ctx context.Context, packet udpClientPacket) error {
	datagram := packet.datagram
	if !a.adoptClientAddr(packet.addr) {
		return nil
	}
	key := udpTargetKey(datagram.Host, datagram.Port)
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
