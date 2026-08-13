package proxy

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rbee3u/efanadp/internal/human"
)

var nextConnID atomic.Uint64

// EventType identifies a connection lifecycle transition.
type EventType int

// Connection lifecycle transitions. EventUnknown is the zero value and is never
// emitted; it exists so an uninitialized Event is not mistaken for an open.
const (
	EventUnknown EventType = iota
	ConnectionOpened
	ConnectionClosed
	ConnectionUpdated
)

// Event is a connection snapshot emitted synchronously by a relay goroutine.
// Stats points to live atomic counters and can continue changing after an open
// or update event is delivered.
type Event struct {
	ID        uint64
	Type      EventType
	Protocol  string
	Source    string
	Target    string
	StartedAt time.Time
	ClosedAt  time.Time
	Stats     *Stats
}

// Stats holds concurrency-safe byte counters for one connection.
type Stats struct {
	upload   atomic.Int64
	download atomic.Int64
}

// AddUpload adds n bytes sent toward the target. Non-positive values and a nil
// receiver are ignored, so relay paths can report short writes unconditionally.
func (s *Stats) AddUpload(n int64) {
	if s != nil && n > 0 {
		s.upload.Add(n)
	}
}

// AddDownload adds n bytes received from the target, with the same nil and
// non-positive handling as AddUpload.
func (s *Stats) AddDownload(n int64) {
	if s != nil && n > 0 {
		s.download.Add(n)
	}
}

// Upload returns the bytes sent so far, or 0 for a nil receiver. The counter
// keeps advancing while a connection is open, so two reads can differ.
func (s *Stats) Upload() int64 {
	if s == nil {
		return 0
	}
	return s.upload.Load()
}

// Download returns the bytes received so far, with the same liveness caveat as
// Upload.
func (s *Stats) Download() int64 {
	if s == nil {
		return 0
	}
	return s.download.Load()
}

type connectionTracker struct {
	cfg       Config
	id        uint64
	protocol  string
	source    string
	startedAt time.Time
	stats     *Stats

	// mu guards target. A UDP association refreshes the label from every target
	// open goroutine and from its client relay loop, so the compare-and-set in
	// updateTarget has to be atomic; two destinations opening at once otherwise
	// race on the field.
	mu     sync.Mutex
	target string
}

func newConnectionTracker(cfg Config, protocol, source, target string) *connectionTracker {
	return &connectionTracker{
		cfg:       cfg,
		id:        nextConnID.Add(1),
		protocol:  protocol,
		source:    source,
		startedAt: time.Now(),
		target:    target,
		stats:     &Stats{},
	}
}

func (t *connectionTracker) open() {
	target := t.currentTarget()
	t.cfg.infof("[%s #%d] open source=%s target=%s", t.protocol, t.id, t.source, target)
	t.cfg.emitConnection(Event{
		ID:        t.id,
		Type:      ConnectionOpened,
		Protocol:  t.protocol,
		Source:    t.source,
		Target:    target,
		StartedAt: t.startedAt,
		Stats:     t.stats,
	})
}

// currentTarget returns the label the connection currently carries.
func (t *connectionTracker) currentTarget() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.target
}

// updateTarget sets the connection label and reports whether it changed. The log
// and event stay outside the lock so an OnConnection callback never runs with it
// held.
func (t *connectionTracker) updateTarget(target string) bool {
	t.mu.Lock()
	if t.target == target {
		t.mu.Unlock()
		return false
	}
	t.target = target
	t.mu.Unlock()

	t.cfg.infof("[%s #%d] update source=%s target=%s", t.protocol, t.id, t.source, target)
	t.cfg.emitConnection(Event{
		ID:        t.id,
		Type:      ConnectionUpdated,
		Protocol:  t.protocol,
		Source:    t.source,
		Target:    target,
		StartedAt: t.startedAt,
		Stats:     t.stats,
	})
	return true
}

func (t *connectionTracker) close(err error) {
	target := t.currentTarget()
	upload := t.stats.Upload()
	download := t.stats.Download()
	if err != nil {
		t.cfg.warnf("[%s #%d] close source=%s target=%s time=%s up=%s down=%s err=%v", t.protocol, t.id, t.source, target, human.ShortDuration(time.Since(t.startedAt)), human.FormatBytes(upload), human.FormatBytes(download), err)
	} else {
		t.cfg.infof("[%s #%d] close source=%s target=%s time=%s up=%s down=%s", t.protocol, t.id, t.source, target, human.ShortDuration(time.Since(t.startedAt)), human.FormatBytes(upload), human.FormatBytes(download))
	}
	t.cfg.emitConnection(Event{
		ID:        t.id,
		Type:      ConnectionClosed,
		Protocol:  t.protocol,
		Source:    t.source,
		Target:    target,
		StartedAt: t.startedAt,
		ClosedAt:  time.Now(),
		Stats:     t.stats,
	})
}
