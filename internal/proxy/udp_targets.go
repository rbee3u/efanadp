package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

type udpTarget struct {
	key            string
	host           string
	port           int
	conn           io.ReadWriteCloser
	responseHeader []byte
}

const udpTargetReadBufferSize = 0xffff

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

func (a *udpAssociation) lookupTarget(key string) (*udpTarget, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.isClosed {
		return nil, net.ErrClosed
	}
	return a.targets[key], nil
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
	return &udpTarget{
		key:            key,
		host:           host,
		port:           port,
		conn:           conn,
		responseHeader: header,
	}, nil
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
			a.removeTarget(target)
			return
		}
		addr := a.getClientAddr()
		if addr == nil {
			continue
		}
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
		close(a.closeDone)
	})
	<-a.closeDone
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
