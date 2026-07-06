package proxy

import (
	"sync/atomic"
	"time"

	"github.com/rbee3u/efanadp/internal/human"
)

var nextConnID atomic.Uint64

type EventType int

const (
	EventUnknown EventType = iota
	ConnectionOpened
	ConnectionClosed
	ConnectionUpdated
)

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

type Stats struct {
	upload   atomic.Int64
	download atomic.Int64
}

func (s *Stats) AddUpload(n int64) {
	if s != nil && n > 0 {
		s.upload.Add(n)
	}
}

func (s *Stats) AddDownload(n int64) {
	if s != nil && n > 0 {
		s.download.Add(n)
	}
}

func (s *Stats) Upload() int64 {
	if s == nil {
		return 0
	}
	return s.upload.Load()
}

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
	target    string
	stats     *Stats
}

func newConnectionTracker(cfg Config, source, target string) *connectionTracker {
	return newProtocolConnectionTracker(cfg, "tcp", source, target)
}

func newUDPConnectionTracker(cfg Config, source, target string) *connectionTracker {
	return newProtocolConnectionTracker(cfg, "udp", source, target)
}

func newProtocolConnectionTracker(cfg Config, protocol, source, target string) *connectionTracker {
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
	t.cfg.infof("[%s #%d] open source=%s target=%s", t.protocol, t.id, t.source, t.target)
	t.cfg.emitConnection(Event{
		ID:        t.id,
		Type:      ConnectionOpened,
		Protocol:  t.protocol,
		Source:    t.source,
		Target:    t.target,
		StartedAt: t.startedAt,
		Stats:     t.stats,
	})
}

func (t *connectionTracker) updateTarget(target string) bool {
	if t.target == target {
		return false
	}
	t.target = target
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
	upload := t.stats.Upload()
	download := t.stats.Download()
	if err != nil {
		t.cfg.warnf("[%s #%d] close source=%s target=%s time=%s up=%s down=%s err=%v", t.protocol, t.id, t.source, t.target, human.ShortDuration(time.Since(t.startedAt)), human.FormatBytes(upload), human.FormatBytes(download), err)
	} else {
		t.cfg.infof("[%s #%d] close source=%s target=%s time=%s up=%s down=%s", t.protocol, t.id, t.source, t.target, human.ShortDuration(time.Since(t.startedAt)), human.FormatBytes(upload), human.FormatBytes(download))
	}
	t.cfg.emitConnection(Event{
		ID:        t.id,
		Type:      ConnectionClosed,
		Protocol:  t.protocol,
		Source:    t.source,
		Target:    t.target,
		StartedAt: t.startedAt,
		ClosedAt:  time.Now(),
		Stats:     t.stats,
	})
}
