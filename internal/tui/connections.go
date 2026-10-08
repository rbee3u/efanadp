package tui

import (
	"cmp"
	"slices"
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
)

// Fallback until terminal dimensions are available.
const connectionPageSize = 30

type connectionItem struct {
	id        uint64
	protocol  string
	source    string
	target    string
	startedAt time.Time
	stats     *proxy.Stats
}

type connectionState struct {
	active   map[uint64]connectionItem
	closed   int
	page     int
	pageSize int
	now      time.Time
}

func (s *connectionState) rowsPerPage() int {
	if s.pageSize > 0 {
		return s.pageSize
	}
	return connectionPageSize
}

func (s *connectionState) setPageSize(size int) {
	s.pageSize = size
	s.page = clampPage(s.page, len(s.active), s.rowsPerPage())
}

func (s *connectionState) reset(now time.Time) {
	s.active = make(map[uint64]connectionItem)
	s.closed = 0
	s.page = 0
	s.now = now
}

func (s *connectionState) apply(event proxy.Event) {
	if s.active == nil {
		s.active = make(map[uint64]connectionItem)
	}
	switch event.Type {
	case proxy.EventUnknown:
		return
	case proxy.ConnectionOpened:
		item := connectionItem{
			id:        event.ID,
			protocol:  event.Protocol,
			source:    event.Source,
			target:    event.Target,
			startedAt: event.StartedAt,
			stats:     event.Stats,
		}
		s.active[event.ID] = item
	case proxy.ConnectionUpdated:
		conn, ok := s.active[event.ID]
		if !ok {
			return
		}
		if event.Source != "" {
			conn.source = event.Source
		}
		if event.Protocol != "" {
			conn.protocol = event.Protocol
		}
		if event.Target != "" {
			conn.target = event.Target
		}
		if !event.StartedAt.IsZero() {
			conn.startedAt = event.StartedAt
		}
		if event.Stats != nil {
			conn.stats = event.Stats
		}
		s.active[event.ID] = conn
	case proxy.ConnectionClosed:
		if _, ok := s.active[event.ID]; ok {
			delete(s.active, event.ID)
			s.closed++
		}
	}
	s.page = clampPage(s.page, len(s.active), s.rowsPerPage())
}

func (s *connectionState) pageItems() ([]connectionItem, int) {
	total := len(s.active)
	start, end, ok := pageRange(s.page, total, s.rowsPerPage())
	if !ok {
		return nil, total
	}
	items := make([]connectionItem, 0, len(s.active))
	for _, conn := range s.active {
		items = append(items, conn)
	}
	slices.SortFunc(items, compareConnections)
	return items[start:end], total
}

func compareConnections(left, right connectionItem) int {
	if order := right.startedAt.Compare(left.startedAt); order != 0 {
		return order
	}
	return cmp.Compare(right.id, left.id)
}

func (s *connectionState) nextPage() {
	s.page = clampPage(s.page+1, len(s.active), s.rowsPerPage())
}

func (s *connectionState) previousPage() {
	s.page = clampPage(s.page-1, len(s.active), s.rowsPerPage())
}
