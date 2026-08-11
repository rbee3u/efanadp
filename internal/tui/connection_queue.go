package tui

import (
	"sync"

	"github.com/rbee3u/efanadp/internal/proxy"
)

// connectionQueue hands proxy connection events to the Bubble Tea update loop
// without dropping any connection lifecycle event.
//
// A bounded channel cannot offer that guarantee. The producer runs on proxy
// connection goroutines and must never block on the UI, so a full channel has
// to discard, and a discarded ConnectionClosed is unrecoverable: the model has
// no other source of truth for a connection ending, so the row stays active
// forever and the closed counter under-counts until the next node switch.
//
// Opened and Closed are therefore always queued. ConnectionUpdated only carries
// the newest label for a connection, so a pending update for the same ID is
// replaced in place instead of appended. Queue growth is then bounded by live
// connections plus the churn between two renders, which the same file
// descriptor limits that bound the proxy already bound.
type connectionQueue struct {
	// signal is a coalescing wakeup: capacity one, non-blocking send. It tells
	// the waiter that pending has something, never how much.
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

// push queues one event. It never blocks and is safe to call after the service
// stops, because connection handlers can outlive the listener.
func (q *connectionQueue) push(event proxy.Event) {
	q.mu.Lock()
	if index, ok := q.updates[event.ID]; ok && event.Type == proxy.ConnectionUpdated {
		q.pending[index] = event
	} else {
		if event.Type == proxy.ConnectionUpdated {
			q.updates[event.ID] = len(q.pending)
		}
		q.pending = append(q.pending, event)
	}
	q.mu.Unlock()

	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// take removes and returns everything queued, or nil when the queue is empty.
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
