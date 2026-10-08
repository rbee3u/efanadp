package session

import (
	"sync"

	"github.com/rbee3u/efanadp/internal/proxy"
)

// connectionQueue preserves lifecycle events without waiting for the UI. Losing
// a close would leave a stale active row. Pending label updates coalesce by ID;
// undrained opens and closes have no fixed bound.
type connectionQueue struct {
	// signal is a coalesced wakeup, not an event count.
	signal chan struct{}

	mu      sync.Mutex
	pending []proxy.Event
	updates map[uint64]int
}

func newConnectionQueue() *connectionQueue {
	return &connectionQueue{
		signal:  make(chan struct{}, 1),
		updates: make(map[uint64]int),
	}
}

// push never waits for the consumer and remains usable after the listener
// stops.
func (q *connectionQueue) push(event proxy.Event) {
	q.mu.Lock()
	switch event.Type {
	case proxy.ConnectionUpdated:
		if index, ok := q.updates[event.ID]; ok {
			q.pending[index] = event
		} else {
			q.updates[event.ID] = len(q.pending)
			q.pending = append(q.pending, event)
		}
	case proxy.ConnectionClosed:
		delete(q.updates, event.ID)
		q.pending = append(q.pending, event)
	case proxy.EventUnknown, proxy.ConnectionOpened:
		q.pending = append(q.pending, event)
	}
	q.mu.Unlock()

	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *connectionQueue) take() []proxy.Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}
	events := q.pending
	q.pending = nil
	clear(q.updates)
	return events
}
