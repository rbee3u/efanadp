package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rbee3u/efanadp/internal/human"
)

type udpTarget struct {
	key            string
	host           string
	port           int
	conn           io.ReadWriteCloser
	responseHeader []byte

	// lastActive is the last time a datagram moved in either direction. The
	// relay goroutine writes it and the sweeper reads it, so it is atomic.
	lastActive atomic.Int64
}

func (t *udpTarget) touch(now time.Time) {
	t.lastActive.Store(now.UnixNano())
}

func (t *udpTarget) idleSince(now time.Time) time.Duration {
	last := t.lastActive.Load()
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

func (a *udpAssociation) getTarget(ctx context.Context, host string, port int) (*udpTarget, bool, error) {
	key := udpTargetKey(host, port)
	target, err := a.lookupTarget(key)
	if err != nil {
		return nil, false, err
	}
	if target != nil {
		return target, false, nil
	}

	target, err = a.openTarget(ctx, key, host, port)
	if err != nil {
		return nil, false, err
	}
	registered, opened, err := a.registerTarget(target)
	if err != nil {
		closeQuietly(target.conn)
		return nil, false, err
	}
	if registered != target {
		closeQuietly(target.conn)
	}
	return registered, opened, nil
}

// lookupTarget returns the cached target for key and marks it active while
// holding mu. Touching here rather than after the lookup returns is what keeps
// the idle sweep from closing a target that a datagram is about to use: the
// sweep takes idle targets under the same lock, so it either observes the fresh
// timestamp or has not yet seen this target at all.
func (a *udpAssociation) lookupTarget(key string) (*udpTarget, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.isClosed {
		return nil, net.ErrClosed
	}
	target := a.targets[key]
	if target != nil {
		target.touch(time.Now())
	}
	return target, nil
}

func (a *udpAssociation) openTarget(ctx context.Context, key, host string, port int) (*udpTarget, error) {
	header, err := buildSocksUDPHeader(host, port)
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

func (a *udpAssociation) noteTargetOpened(key string) {
	a.mu.Lock()
	if _, ok := a.seenTargets[key]; ok {
		a.mu.Unlock()
		return
	}
	a.seenTargets[key] = struct{}{}
	targetCount := len(a.seenTargets)
	targetSummary := key
	if targetCount > 1 {
		targetSummary = fmt.Sprintf("%s (+%d more)", key, targetCount-1)
	}
	a.mu.Unlock()

	a.cfg.infof("[udp] open target=%s", key)
	if a.tracker != nil {
		a.tracker.updateTarget(targetSummary)
	}
}

func (a *udpAssociation) noteInnerTarget(target string) {
	if a.tracker == nil {
		return
	}
	if a.tracker.updateTarget(target) {
		a.cfg.infof("[udp] sniff target=%s", target)
	}
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
