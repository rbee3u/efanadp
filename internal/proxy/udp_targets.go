package proxy

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rbee3u/efanadp/internal/datagram"
	"github.com/rbee3u/efanadp/internal/human"
	"github.com/rbee3u/efanadp/internal/sniff"
)

type udpTarget struct {
	key            string
	conn           datagram.Conn
	responseHeader []byte

	// Cache derived keys per target; serialize access from opening and relay
	// goroutines.
	sniffMu sync.Mutex
	sniffer *sniff.ShadowsocksUDPSniffer

	// lastActive tracks traffic in either direction for the concurrent idle
	// sweeper.
	lastActive atomic.Int64
}

func (ut *udpTarget) touch(now time.Time) {
	ut.lastActive.Store(now.UnixNano())
}

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

// DefaultUDPTargetIdleTimeout releases quiet streams within long-lived
// associations.
const DefaultUDPTargetIdleTimeout = 90 * time.Second

const udpTargetSweepInterval = 15 * time.Second

func udpTargetKey(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Allow bursts such as concurrent DNS queries without unbounded buffering.
const maxPendingTargetDatagrams = 8

// Count open and opening targets together to bound streams and concurrent
// handshakes.
const maxUDPTargetsPerAssociation = 64

type openingTarget struct {
	pending [][]byte
}

func (o *openingTarget) enqueue(payload []byte) bool {
	if len(o.pending) >= maxPendingTargetDatagrams {
		return false
	}
	// Copy before the relay loop reuses its receive buffer.
	o.pending = append(o.pending, bytes.Clone(payload))
	return true
}

// takeTargetForDatagram returns an open target, queues the payload for a
// background open, or reports a drop reason. Opens must not block other
// destinations on the client reader. Lookup and open registration share one
// lock to prevent duplicate opens or orphaned queues.
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
	// Refresh under the sweep lock so reuse cannot race idle eviction.
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
	// Register the goroutine under mu before close can start waiting.
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
		if !a.shouldStopRelay(err) {
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

// Keep the opening entry until its queue is empty under mu, so later datagrams
// cannot overtake queued ones.
func (a *udpAssociation) drainPendingDatagrams(key string, target *udpTarget) {
	for {
		pending := a.takePendingDatagrams(key)
		if pending == nil {
			return
		}
		for _, payload := range pending {
			if !a.sendToTarget(target, payload) {
				a.abandonOpen(key)
				return
			}
		}
	}
}

func (a *udpAssociation) takePendingDatagrams(key string) [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	opening := a.openingTargets[key]
	if opening == nil {
		return nil
	}
	if len(opening.pending) == 0 {
		delete(a.openingTargets, key)
		return nil
	}
	pending := opening.pending
	opening.pending = nil
	return pending
}

func (a *udpAssociation) abandonOpen(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.openingTargets, key)
}

// Write failures detach the target so the next datagram can reopen it.
func (a *udpAssociation) sendToTarget(target *udpTarget, payload []byte) bool {
	if innerTarget, ok := target.sniff(payload); ok {
		a.noteInnerTarget(target.key, innerTarget)
	}
	if _, err := target.conn.WriteDatagram(payload); err != nil {
		if !a.shouldStopRelay(err) {
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
	conn, err := a.cfg.openUDP(ctx, host, port)
	if err != nil {
		return nil, err
	}
	target := &udpTarget{
		key:            key,
		conn:           conn,
		responseHeader: header,
		sniffer:        sniff.NewShadowsocksUDPSniffer(a.cfg.ShadowsocksSniffConfigs, host, port),
	}
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

func (a *udpAssociation) relayFromTarget(target *udpTarget) {
	defer a.removeTarget(target)

	headerLen := len(target.responseHeader)
	buf := make([]byte, headerLen+udpTargetReadBufferSize)
	copy(buf, target.responseHeader)
	for {
		n, err := target.conn.ReadDatagram(buf[headerLen:])
		if err != nil {
			// Deferred upstream refusal appears on this first read; routine
			// closure stays quiet.
			if !a.isContextDone() && !isBenignClientError(err) {
				a.cfg.warnf("[udp] read response target=%s err=%v", target.key, err)
			}
			return
		}
		addr := a.pinnedClientAddr()
		if addr == nil {
			continue
		}
		target.touch(time.Now())
		packet := buf[:headerLen+n]
		if _, err := a.udpConn.WriteToUDP(packet, addr); err != nil {
			// A local send failure affects one datagram, not the cached
			// upstream. The SOCKS envelope can push an X365 response past the
			// local UDP packet limit.
			if a.shouldStopRelay(err) {
				return
			}
			if a.warnDropOnce(dropReasonClientWrite) {
				a.cfg.warnf("[udp] drop response target=%s reason=%s err=%v", target.key, dropReasonClientWrite, err)
			}
			continue
		}
		a.stats.AddDownload(int64(n))
	}
}

func (a *udpAssociation) removeTarget(target *udpTarget) {
	if a.detachTarget(target) {
		closeQuietly(target.conn)
	}
}

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
		for _, target := range a.markClosedAndTakeTargets() {
			closeQuietly(target.conn)
		}
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
