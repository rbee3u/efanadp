package session

import (
	"fmt"
	"slices"
	"testing"

	"github.com/rbee3u/efanadp/internal/proxy"
)

func TestConnectionQueueKeepsEveryLifecycleEventUnderBurst(t *testing.T) {
	events := newConnectionQueue()
	const connections = 5000
	for i := range connections {
		id := uint64(i + 1)
		events.push(proxy.Event{ID: id, Type: proxy.ConnectionOpened, Stats: &proxy.Stats{}})
		events.push(proxy.Event{ID: id, Type: proxy.ConnectionClosed, Stats: &proxy.Stats{}})
	}

	batch := events.take()
	if len(batch) != connections*2 {
		t.Fatalf("events=%d, want %d", len(batch), connections*2)
	}
	for i := range connections {
		if batch[2*i].ID != uint64(i+1) || batch[2*i].Type != proxy.ConnectionOpened || batch[2*i+1].ID != uint64(i+1) || batch[2*i+1].Type != proxy.ConnectionClosed {
			t.Fatalf("lost lifecycle pair %d", i)
		}
	}
}

func TestConnectionQueueCoalescesRepeatedUpdates(t *testing.T) {
	queue := newConnectionQueue()
	opened := proxy.Event{ID: 1, Type: proxy.ConnectionOpened, Target: "example.org:443"}
	other := proxy.Event{ID: 2, Type: proxy.ConnectionOpened, Target: "example.net:80"}
	updated := proxy.Event{ID: 1, Type: proxy.ConnectionUpdated, Target: "example.org:443->example.net:1099"}
	otherUpdated := proxy.Event{ID: 2, Type: proxy.ConnectionUpdated, Target: "example.net:80->example.org:80"}
	queue.push(opened)
	queue.push(other)
	for i := range 100 {
		queue.push(proxy.Event{
			ID: 1, Type: proxy.ConnectionUpdated,
			Target: fmt.Sprintf("example.org:443->example.net:%d", 1000+i),
		})
		queue.push(otherUpdated)
	}

	batch := queue.take()
	want := []proxy.Event{opened, other, updated, otherUpdated}
	if !slices.Equal(batch, want) {
		t.Fatalf("batch=%+v, want independent latest updates in order %+v", batch, want)
	}
	if extra := queue.take(); len(extra) != 0 {
		t.Fatalf("drained queue returned duplicate events: %+v", extra)
	}

	// A later batch must neither reuse old update indexes nor change snapshots
	// already handed to the consumer.
	next := proxy.Event{ID: 1, Type: proxy.ConnectionUpdated, Target: "example.org:443->example.net:2000"}
	closed := proxy.Event{ID: 1, Type: proxy.ConnectionClosed, Target: next.Target}
	queue.push(next)
	queue.push(closed)
	if got := queue.take(); !slices.Equal(got, []proxy.Event{next, closed}) {
		t.Fatalf("next batch=%+v, want update followed by close", got)
	}
	if !slices.Equal(batch, want) {
		t.Fatalf("previous batch mutated: %+v, want %+v", batch, want)
	}
}

func TestConnectionQueueForgetsClosedConnections(t *testing.T) {
	queue := newConnectionQueue()
	const connections = 100

	for i := range connections {
		id := uint64(i + 1)
		queue.push(proxy.Event{ID: id, Type: proxy.ConnectionOpened, Target: "example.org:443"})
		for j := range 10 {
			queue.push(proxy.Event{
				ID:     id,
				Type:   proxy.ConnectionUpdated,
				Target: fmt.Sprintf("example.org:443->update-%d", j),
			})
		}
		queue.push(proxy.Event{ID: id, Type: proxy.ConnectionClosed})
	}

	queue.mu.Lock()
	tracked := len(queue.updates)
	queue.mu.Unlock()
	if tracked != 0 {
		t.Fatalf("tracked update entries=%d after every connection closed, want 0", tracked)
	}

	// Coalescing must not lose the lifecycle events themselves.
	events := queue.take()
	if len(events) != connections*3 {
		t.Fatalf("queued events=%d, want %d (open + one coalesced update + close each)", len(events), connections*3)
	}
}
