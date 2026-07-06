package proxy

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"

	"github.com/rbee3u/efanadp/internal/x365"
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
		writeSOCKSReplyQuietly(client, 5)
		return nil
	}

	if err := writeSocksReplyAddr(client, 0, udpConn.LocalAddr()); err != nil {
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
	defer cancel()
	assoc := newUDPAssociation(assocCtx.Done(), cfg, udpConn, tracker)
	defer assoc.close()

	relayErr = assoc.relay(assocCtx, cancel, client, reader)

	return nil
}

type udpAssociation struct {
	cfg     Config
	ctxDone <-chan struct{}
	udpConn udpPacketConn
	tracker *connectionTracker
	stats   *Stats

	mu          sync.Mutex
	clientAddr  *net.UDPAddr
	targets     map[string]*udpTarget
	seenTargets map[string]struct{}
	isClosed    bool

	targetGroup sync.WaitGroup
	closeOnce   sync.Once
	closeDone   chan struct{}
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
		cfg:         cfg,
		ctxDone:     done,
		udpConn:     udpConn,
		tracker:     tracker,
		targets:     make(map[string]*udpTarget),
		seenTargets: make(map[string]struct{}),
		stats:       stats,
		closeDone:   make(chan struct{}),
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

	datagram, err := parseSocksUDPDatagram(buf[:n])
	if err != nil {
		a.cfg.warnf("[udp] drop malformed SOCKS5 datagram from=%s err=%v", addr, err)
		return udpClientPacket{}, false, nil
	}
	if a.shouldDropClientDatagram(datagram) {
		return udpClientPacket{}, false, nil
	}
	return udpClientPacket{addr: addr, datagram: datagram}, true, nil
}

func (a *udpAssociation) shouldDropClientDatagram(datagram socksUDPDatagram) bool {
	target := udpTargetKey(datagram.Host, datagram.Port)
	if datagram.Frag != 0 {
		a.cfg.warnf("[udp] drop fragmented SOCKS5 datagram frag=%d target=%s", datagram.Frag, target)
		return true
	}
	if datagram.Port == 443 {
		a.cfg.warnf("[udp] drop target=%s reason=x365-udp-443-disabled", target)
		return true
	}
	if isFakeIPHost(datagram.Host) {
		a.cfg.warnf("[udp] drop fake-ip target=%s", target)
		return true
	}
	return false
}

func (a *udpAssociation) forwardClientPacket(ctx context.Context, packet udpClientPacket) error {
	datagram := packet.datagram
	a.setClientAddr(packet.addr)
	target, opened, err := a.getTarget(ctx, datagram.Host, datagram.Port)
	if err != nil {
		if a.shouldStopRelay(err) {
			return err
		}
		a.cfg.warnf("[udp] open target=%s err=%v", udpTargetKey(datagram.Host, datagram.Port), err)
		return nil
	}
	if opened {
		a.noteTargetOpened(target.key)
	}
	if innerTarget, ok := x365.SniffShadowsocksUDPChain(
		a.cfg.ShadowsocksSniffConfigs,
		target.host,
		target.port,
		datagram.Payload,
	); ok {
		a.noteInnerTarget(x365.FormatTargetChain(target.key, innerTarget))
	}
	if _, err := target.conn.Write(datagram.Payload); err != nil {
		a.cfg.warnf("[udp] write target=%s err=%v", target.key, err)
		a.removeTarget(target)
		return nil
	}
	a.stats.AddUpload(int64(len(datagram.Payload)))
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

func (a *udpAssociation) setClientAddr(addr *net.UDPAddr) {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := *addr
	copied.IP = slices.Clone(addr.IP)
	a.clientAddr = &copied
}

func (a *udpAssociation) getClientAddr() *net.UDPAddr {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.clientAddr == nil {
		return nil
	}
	copied := *a.clientAddr
	copied.IP = slices.Clone(a.clientAddr.IP)
	return &copied
}

func udpBindAddrForClient(client net.Conn) *net.UDPAddr {
	ip := net.IPv4(127, 0, 0, 1)
	if tcpAddr, ok := client.LocalAddr().(*net.TCPAddr); ok && tcpAddr.IP != nil && !tcpAddr.IP.IsUnspecified() {
		ip = tcpAddr.IP
	}
	return &net.UDPAddr{IP: ip, Port: 0}
}
