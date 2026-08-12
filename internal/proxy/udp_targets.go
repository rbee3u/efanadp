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
	"sync/atomic"
	"time"

	"github.com/rbee3u/efanadp/internal/human"
	"github.com/rbee3u/efanadp/internal/x365"
)

type udpTarget struct {
	key            string
	host           string
	port           int
	conn           io.ReadWriteCloser
	responseHeader []byte

	// sniffer is nil unless this destination is a configured Shadowsocks
	// server. It holds the derived key material for that server, so only the
	// per-datagram steps run per datagram.
	//
	// sniffMu guards it because two goroutines can reach one target: the open
	// goroutine sniffs the datagram that triggered the open, and the client relay
	// goroutine may already be sniffing a later datagram for the same
	// destination. ShadowsocksUDPSniffer is not safe for concurrent use, and the
	// lock is uncontended next to the AEAD open it protects.
	sniffMu sync.Mutex
	sniffer *x365.ShadowsocksUDPSniffer

	// lastActive is the last time a datagram moved in either direction. The
	// relay goroutine writes it and the sweeper reads it, so it is atomic.
	lastActive atomic.Int64
}

func (ut *udpTarget) touch(now time.Time) {
	ut.lastActive.Store(now.UnixNano())
}

// sniff reports the inner destination carried by one datagram, or false when
// this destination is not a configured Shadowsocks server.
func (ut *udpTarget) sniff(payload []byte) (string, bool) {
	if ut.sniffer == nil {
		return "", false
	}
	ut.sniffMu.Lock()
	defer ut.sniffMu.Unlock()
	return ut.sniffer.Sniff(payload)
}

func (ut *udpTarget) idleSince(now time.Time) time.Duration {
	last := ut.lastActive.Load()
	if last == 0 {
		return 0
	}
	return now.Sub(time.Unix(0, last))
}

const udpTargetReadBufferSize = 0xffff

// DefaultUDPTargetIdleTimeout bounds how long an unused UDP destination keeps
// its upstream stream open. Each cached target owns an xhttp + REALITY stream, a
// relay goroutine, and a 64 KiB buffer, and an association is only torn down
// when the client closes its TCP control connection. A long-lived association
// that talks to many destinations would otherwise hold every one of them for its
// whole lifetime. The timeout is long enough that steady DNS traffic keeps its
// resolver target warm instead of reopening a stream per query.
const DefaultUDPTargetIdleTimeout = 90 * time.Second

const udpTargetSweepInterval = 15 * time.Second

func udpTargetKey(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// maxPendingTargetDatagrams bounds how many datagrams wait on one destination's
// open. A handful is enough for the case that matters: a resolver sending its A,
// AAAA, and HTTPS queries back to back, or a client retrying while the first
// open is still in flight. Past that the client is outrunning a destination that
// is not answering yet, and holding more datagrams only spends memory to deliver
// a burst the client has already given up on.
const maxPendingTargetDatagrams = 8

// maxUDPTargetsPerAssociation bounds the streams and in-flight handshakes one
// local client can make a single association hold. Every distinct destination
// otherwise starts a background TCP dial and REALITY handshake immediately,
// and an open target retains its stream, relay goroutine, and read buffer until
// it goes idle. The bound counts both states so a burst of unique destinations
// cannot outrun the cache before any handshake finishes.
const maxUDPTargetsPerAssociation = 64

// openingTarget holds the datagrams waiting on one destination's open.
type openingTarget struct {
	pending [][]byte
}

// enqueue copies payload onto the queue and reports whether it fit.
func (o *openingTarget) enqueue(payload []byte) bool {
	if len(o.pending) >= maxPendingTargetDatagrams {
		return false
	}
	// ReadFromUDP reuses its buffer, so the payload must be copied before the
	// relay loop reads the next datagram over it.
	o.pending = append(o.pending, bytes.Clone(payload))
	return true
}

// takeTargetForDatagram decides what becomes of one client datagram, returning a
// target only when the destination is already open and the caller should write
// immediately. Otherwise the datagram has been handed to an open, either one
// already in flight or one this call starts, and the caller has nothing left to
// do. The string reports why the datagram was dropped, or is empty when the
// datagram was accepted.
//
// The open cannot run on the client relay loop. It dials a TCP connection,
// completes a REALITY handshake, negotiates HTTP/2, and writes the X365 request,
// which is one or more round trips on a good path and the full open timeout on a
// blackholed one. That loop is the only reader of the association socket, so
// every other destination's datagrams wait behind whichever open is in flight,
// and the kernel receive buffer drops them once it fills. A destination the
// server refuses makes it repeat: the open itself succeeds because the X365
// response is read lazily, the first target read then fails and detaches it, and
// the next datagram opens it again.
//
// Queueing rather than dropping is what keeps that off-loop open cheap for the
// traffic UDP_ASSOCIATE actually carries. A stub resolver sends its A and AAAA
// queries together, so with a drop the second one is lost on every first lookup
// through a destination and the client waits out its retry timeout, turning one
// handshake into seconds of added latency.
//
// Deciding in one locked step is what makes the choice correct. Looking the
// target up and then claiming the open separately would let an open complete in
// between, so the datagram would either start a redundant dial for a destination
// that is already open or join a queue no one is left to flush.
func (a *udpAssociation) takeTargetForDatagram(
	ctx context.Context,
	key, host string,
	port int,
	payload []byte,
) (*udpTarget, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.isClosed {
		return nil, "", net.ErrClosed
	}
	if opening := a.openingTargets[key]; opening != nil {
		if !opening.enqueue(payload) {
			return nil, dropReasonTargetNotReady, nil
		}
		return nil, "", nil
	}
	// Touching here rather than after the lookup returns is what keeps the idle
	// sweep from closing a target that a datagram is about to use: the sweep takes
	// idle targets under this same lock, so it either observes the fresh timestamp
	// or has not yet seen this target at all.
	if target := a.targets[key]; target != nil {
		target.touch(time.Now())
		return target, "", nil
	}
	if a.distinctTargetCountLocked() >= maxUDPTargetsPerAssociation {
		return nil, dropReasonTargetLimit, nil
	}

	opening := &openingTarget{}
	opening.enqueue(payload)
	a.openingTargets[key] = opening
	// Start while holding mu so close cannot mark the association closed between
	// the check above and targetGroup tracking this goroutine.
	a.targetGroup.Go(func() {
		a.openTargetAndDrain(ctx, key, host, port)
	})
	return nil, "", nil
}

// distinctTargetCountLocked counts destination keys rather than map entries.
// A target remains in openingTargets while its queued datagrams drain after it
// is registered, so the same key can briefly exist in both maps.
func (a *udpAssociation) distinctTargetCountLocked() int {
	count := len(a.targets)
	for key := range a.openingTargets {
		if a.targets[key] == nil {
			count++
		}
	}
	return count
}

func (a *udpAssociation) openTargetAndDrain(ctx context.Context, key, host string, port int) {
	target, err := a.openTarget(ctx, key, host, port)
	if err != nil {
		if !a.isContextDone() && !errors.Is(err, net.ErrClosed) {
			a.cfg.warnf("[udp] open target=%s err=%v", key, err)
		}
		a.abandonOpen(key)
		return
	}
	registered, opened, err := a.registerTarget(target)
	if err != nil {
		closeQuietly(target.conn)
		a.abandonOpen(key)
		return
	}
	if registered != target {
		closeQuietly(target.conn)
	}
	if opened {
		a.noteTargetOpened(registered.key)
	}
	a.drainPendingDatagrams(key, registered)
}

// drainPendingDatagrams forwards everything queued for key, then releases the
// open so the relay loop starts writing to this destination itself.
//
// Draining until the queue is empty and releasing in the same locked step is
// what keeps datagrams in order. Releasing the open as soon as the target is
// registered would let the relay loop write a later datagram while this
// goroutine is still writing an earlier one.
func (a *udpAssociation) drainPendingDatagrams(key string, target *udpTarget) {
	for {
		pending, done := a.takePendingDatagrams(key)
		if done {
			return
		}
		for _, payload := range pending {
			if !a.sendToTarget(target, payload) {
				// The destination is already detached, so the rest of the queue has
				// nowhere to go and a later datagram reopens it.
				a.abandonOpen(key)
				return
			}
		}
	}
}

// takePendingDatagrams returns the datagrams queued for key, or reports done
// after releasing the open once nothing is left.
func (a *udpAssociation) takePendingDatagrams(key string) ([][]byte, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	opening := a.openingTargets[key]
	if opening == nil {
		return nil, true
	}
	if len(opening.pending) == 0 {
		delete(a.openingTargets, key)
		return nil, true
	}
	pending := opening.pending
	opening.pending = nil
	return pending, false
}

// abandonOpen releases a destination's open along with anything still queued for
// it, so a later datagram can try again.
func (a *udpAssociation) abandonOpen(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.openingTargets, key)
}

// sendToTarget forwards one datagram payload to an open destination and reports
// whether it went out, detaching the destination when the upstream write fails
// so a later datagram reopens it.
func (a *udpAssociation) sendToTarget(target *udpTarget, payload []byte) bool {
	if innerTarget, ok := target.sniff(payload); ok {
		a.noteInnerTarget(x365.FormatTargetChain(target.key, innerTarget))
	}
	if _, err := target.conn.Write(payload); err != nil {
		if !a.isContextDone() && !errors.Is(err, net.ErrClosed) {
			a.cfg.warnf("[udp] write target=%s err=%v", target.key, err)
		}
		a.removeTarget(target)
		return false
	}
	a.stats.AddUpload(int64(len(payload)))
	return true
}

func (a *udpAssociation) openTarget(ctx context.Context, key, host string, port int) (*udpTarget, error) {
	header, err := buildSOCKSUDPHeader(host, port)
	if err != nil {
		return nil, err
	}
	conn, err := a.cfg.dialUDP(ctx, host, port)
	if err != nil {
		return nil, err
	}
	target := &udpTarget{
		key:            key,
		host:           host,
		port:           port,
		conn:           conn,
		responseHeader: header,
	}
	target.sniffer = x365.NewShadowsocksUDPSniffer(a.cfg.ShadowsocksSniffConfigs, target.host, target.port)
	target.touch(time.Now())
	return target, nil
}

func (a *udpAssociation) registerTarget(target *udpTarget) (*udpTarget, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.isClosed {
		return nil, false, net.ErrClosed
	}
	if existing := a.targets[target.key]; existing != nil {
		return existing, false, nil
	}
	a.targets[target.key] = target
	// Start while holding mu so close cannot observe the target before
	// targetGroup tracks its relay goroutine.
	a.targetGroup.Go(func() {
		a.relayFromTarget(target)
	})

	return target, true, nil
}

// noteTargetOpened logs a newly opened destination and refreshes the connection
// label.
//
// The count comes from the live target map rather than a tally of everything
// ever opened, so "(+N more)" describes what the association currently holds.
// The idle sweep closes quiet destinations and a later datagram reopens them, so
// a cumulative tally drifts above the live count for the rest of the
// association's life. Each open is logged for the same reason: a "close idle"
// line with no matching reopen reads as a destination that never came back.
func (a *udpAssociation) noteTargetOpened(key string) {
	a.mu.Lock()
	liveCount := len(a.targets)
	if a.label == "" {
		a.label = key
	}
	a.mu.Unlock()

	targetSummary := key
	if liveCount > 1 {
		targetSummary = fmt.Sprintf("%s (+%d more)", key, liveCount-1)
	}

	a.cfg.infof("[udp] open target=%s", key)
	if a.tracker != nil {
		a.tracker.updateTarget(targetSummary)
	}
}

// maxSniffedInnerTargets bounds how many distinct inner destinations one
// association reports. A Shadowsocks server relaying UDP for many peers can
// produce an unbounded number of them, and each one costs a log line, an event,
// and a map entry.
const maxSniffedInnerTargets = 64

// noteInnerTarget reports a sniffed inner destination the first time this
// association sees it, and warns once if the label is oscillating.
//
// One association can carry many inner destinations through a single
// Shadowsocks server, and the label only holds one at a time. Reporting every
// change would make each datagram that switches destination a fresh log line
// and TUI event, so a chain is reported once and the label settles on the most
// recently discovered one.
func (a *udpAssociation) noteInnerTarget(target string) {
	if !a.recordInnerTarget(target) {
		return
	}

	a.mu.Lock()
	// The connection label holds the outer target until the first inner one is
	// discovered, so only a change between two known chains is an oscillation.
	previous := a.label
	oscillating := previous != "" && !isOuterTarget(previous) && previous != target
	warn := oscillating && !a.warnedLabelOscillation
	if warn {
		a.warnedLabelOscillation = true
	}
	a.label = target
	a.mu.Unlock()

	if warn {
		a.cfg.warnf("[udp] label oscillation old=%s new=%s", previous, target)
	}
	if a.tracker != nil && a.tracker.updateTarget(target) {
		a.cfg.infof("[udp] sniff target=%s", target)
	}
}

// isOuterTarget reports whether target is just a host:port destination rather
// than a chained Shadowsocks target. The label starts as the outer target set by
// noteTargetOpened, then switches to an inner chain as soon as one is sniffed;
// that switch is the natural flow and should not warn as oscillation.
func isOuterTarget(target string) bool {
	return !strings.Contains(target, "->")
}

// recordInnerTarget returns true if target should be reported as a newly
// discovered chain, bounded by maxSniffedInnerTargets.
func (a *udpAssociation) recordInnerTarget(target string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.sniffedInnerTargets) >= maxSniffedInnerTargets {
		return false
	}
	if _, seen := a.sniffedInnerTargets[target]; seen {
		return false
	}
	a.sniffedInnerTargets[target] = struct{}{}
	return true
}

func (a *udpAssociation) relayFromTarget(target *udpTarget) {
	headerLen := len(target.responseHeader)
	buf := make([]byte, headerLen+udpTargetReadBufferSize)
	copy(buf, target.responseHeader)
	for {
		n, err := target.conn.Read(buf[headerLen:])
		if err != nil {
			// openUDP defers its X365 response read to the first Read here, so a
			// rejected or failed upstream surfaces only on this path. Without a
			// log, the client just sees no reply and the log shows nothing after
			// "open target=". Close, idle sweep, and EOF stay quiet.
			if !a.isContextDone() && !isBenignClientError(err) {
				a.cfg.warnf("[udp] read response target=%s err=%v", target.key, err)
			}
			a.removeTarget(target)
			return
		}
		addr := a.getClientAddr()
		if addr == nil {
			continue
		}
		target.touch(time.Now())
		packet := buf[:headerLen+n]
		if _, err := a.udpConn.WriteToUDP(packet, addr); err != nil {
			if !a.isContextDone() && !errors.Is(err, net.ErrClosed) {
				a.cfg.warnf("[udp] write response target=%s err=%v", target.key, err)
			}
			a.removeTarget(target)
			return
		}
		a.stats.AddDownload(int64(n))
	}
}

func (a *udpAssociation) removeTarget(target *udpTarget) {
	if a.detachTarget(target) {
		closeQuietly(target.conn)
	}
}

// sweepIdleTargets closes cached destinations that have gone quiet. Closing the
// stream unblocks that target's relayFromTarget, which detaches it; a later
// datagram for the same destination simply reopens it.
func (a *udpAssociation) sweepIdleTargets(ctx context.Context, idleTimeout time.Duration) {
	if idleTimeout <= 0 {
		return
	}
	interval := min(udpTargetSweepInterval, idleTimeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, target := range a.takeIdleTargets(now, idleTimeout) {
				a.cfg.infof("[udp] close idle target=%s idle=%s", target.key, human.ShortDuration(target.idleSince(now)))
				closeQuietly(target.conn)
			}
		}
	}
}

func (a *udpAssociation) takeIdleTargets(now time.Time, idleTimeout time.Duration) []*udpTarget {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.isClosed {
		return nil
	}
	var idle []*udpTarget
	for key, target := range a.targets {
		if target.idleSince(now) < idleTimeout {
			continue
		}
		delete(a.targets, key)
		idle = append(idle, target)
	}
	return idle
}

func (a *udpAssociation) detachTarget(target *udpTarget) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.targets[target.key] != target {
		return false
	}
	delete(a.targets, target.key)
	return true
}

func (a *udpAssociation) close() {
	a.closeOnce.Do(func() {
		closeQuietly(a.udpConn)
		closeUDPTargets(a.markClosedAndTakeTargets())
		a.targetGroup.Wait()
		a.sweeperGroup.Wait()
	})
}

func (a *udpAssociation) markClosedAndTakeTargets() []*udpTarget {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.isClosed = true
	targets := make([]*udpTarget, 0, len(a.targets))
	for _, target := range a.targets {
		targets = append(targets, target)
	}
	a.targets = make(map[string]*udpTarget)
	return targets
}

func closeUDPTargets(targets []*udpTarget) {
	for _, target := range targets {
		closeQuietly(target.conn)
	}
}
