package proxy

import "fmt"

// udpObservation tracks display-only labels and bounded reporting history.
// Its owner serializes access; it neither owns sockets nor emits callbacks.
type udpObservation struct {
	innerTargets      map[string]struct{}
	warnedDrops       map[string]struct{}
	warnedOscillation bool
	label             string
	labelIsInner      bool
}

// Bound memory and event volume even when many inner targets share one server.
const maxSniffedInnerTargets = 64

func newUDPObservation() udpObservation {
	return udpObservation{innerTargets: make(map[string]struct{}), warnedDrops: make(map[string]struct{})}
}

func (o *udpObservation) noteOuterTarget(target string) {
	if o.label == "" {
		o.label = target
	}
}

// Deduplicate chains to avoid per-packet UI updates; warn only on the first
// change between sniffed labels.
func (o *udpObservation) noteInnerTarget(target string, isInner bool) (fresh bool, previous string, warn bool) {
	if len(o.innerTargets) >= maxSniffedInnerTargets {
		return false, "", false
	}
	if _, seen := o.innerTargets[target]; seen {
		return false, "", false
	}
	o.innerTargets[target] = struct{}{}
	previous = o.label
	warn = o.labelIsInner && previous != target && !o.warnedOscillation
	if warn {
		o.warnedOscillation = true
	}
	o.label = target
	o.labelIsInner = isInner
	return true, previous, warn
}

func (o *udpObservation) warnDropOnce(reason string) bool {
	if _, seen := o.warnedDrops[reason]; seen {
		return false
	}
	o.warnedDrops[reason] = struct{}{}
	return true
}

func (a *udpAssociation) warnDropOnce(reason string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.observation.warnDropOnce(reason)
}

// Count live targets, including reopens, so the label does not accumulate
// history.
func (a *udpAssociation) noteTargetOpened(key string) {
	a.mu.Lock()
	liveCount := len(a.targets)
	a.observation.noteOuterTarget(key)
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

// Run callbacks outside the association lock to allow reentry.
func (a *udpAssociation) noteInnerTarget(outer, inner string) {
	target := formatTargetChain(outer, inner)
	a.mu.Lock()
	fresh, previous, warn := a.observation.noteInnerTarget(target, inner != "" && inner != outer)
	a.mu.Unlock()
	if !fresh {
		return
	}
	if warn {
		a.cfg.warnf("[udp] label oscillation old=%s new=%s", previous, target)
	}
	if a.tracker != nil && a.tracker.updateTarget(target) {
		a.cfg.infof("[udp] sniff target=%s", target)
	}
}
