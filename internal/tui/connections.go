package tui

import (
	"cmp"
	"slices"
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
)

// connectionPageSize is the fallback page size used until the terminal
// dimensions are known; syncConnectionPageSize keeps pageSize in step with
// the space available in the main pane.
const connectionPageSize = 30

type connectionState struct {
	active   map[uint64]connectionItem
	closed   int
	page     int
	pageSize int
	now      time.Time
}

func (s *connectionState) size() int {
	if s.pageSize > 0 {
		return s.pageSize
	}
	return connectionPageSize
}

func (s *connectionState) setPageSize(size int) {
	s.pageSize = size
	s.page = clampPage(s.page, s.activeCount(), s.size())
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
	s.page = clampPage(s.page, s.activeCount(), s.size())
}

func (s *connectionState) activeItems() []connectionItem {
	items := make([]connectionItem, 0, len(s.active))
	for _, conn := range s.active {
		items = append(items, conn)
	}
	sortConnections(items)
	return items
}

func (s *connectionState) pageItems(page int) ([]connectionItem, int) {
	total := len(s.active)
	start, end, ok := pageRange(page, total, s.size())
	if !ok {
		return nil, total
	}
	items := s.activeItems()
	return items[start:end], total
}

func sortConnections(items []connectionItem) {
	slices.SortFunc(items, compareConnections)
}

func compareConnections(left, right connectionItem) int {
	if left.startedAt.Equal(right.startedAt) {
		return cmp.Compare(right.id, left.id)
	}
	if left.startedAt.After(right.startedAt) {
		return -1
	}
	return 1
}

func (s *connectionState) activeCount() int {
	return len(s.active)
}

func (s *connectionState) nextPage() bool {
	next := clampPage(s.page+1, s.activeCount(), s.size())
	if next == s.page {
		return false
	}
	s.page = next
	return true
}

func (s *connectionState) previousPage() bool {
	next := clampPage(s.page-1, s.activeCount(), s.size())
	if next == s.page {
		return false
	}
	s.page = next
	return true
}
