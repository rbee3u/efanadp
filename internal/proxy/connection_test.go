package proxy

import (
	"fmt"
	"sync"
	"testing"
)

func TestConnectionTrackerCloseUsesUpdatedTarget(t *testing.T) {
	var events []Event
	tracker := newConnectionTracker(Config{
		OnConnection: func(event Event) {
			events = append(events, event)
		},
	}, "tcp", "127.0.0.1:53000", "203.0.113.10:39281")

	tracker.open()
	tracker.updateTarget("203.0.113.10:39281->ifconfig.me:443")
	tracker.close(nil)

	if len(events) != 3 {
		t.Fatalf("len(events)=%d, want 3", len(events))
	}
	if events[0].Target != "203.0.113.10:39281" {
		t.Fatalf("open target=%q, want outer target", events[0].Target)
	}
	if events[1].Target != "203.0.113.10:39281->ifconfig.me:443" {
		t.Fatalf("update target=%q, want updated target", events[1].Target)
	}
	if events[2].Target != "203.0.113.10:39281->ifconfig.me:443" {
		t.Fatalf("close target=%q, want updated target", events[2].Target)
	}
}

func TestConnectionTrackerUpdateTargetIsThreadSafe(t *testing.T) {
	var mu sync.Mutex
	tracker := newConnectionTracker(Config{
		OnConnection: func(Event) {
			mu.Lock()
			defer mu.Unlock()
		},
	}, "udp", "127.0.0.1:53000", "203.0.113.10:39281")

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			tracker.updateTarget(fmt.Sprintf("203.0.113.10:39281->peer%d.example:5353", i))
		})
	}
	wg.Wait()

	if tracker.currentTarget() == "" {
		t.Fatal("tracker target is empty after concurrent updates")
	}
}

func TestFormatTargetChain(t *testing.T) {
	if got := formatTargetChain("outer.example:443", "inner.example:443"); got != "outer.example:443->inner.example:443" {
		t.Fatalf("target chain=%q, want outer->inner", got)
	}
	if got := formatTargetChain("outer.example:443", ""); got != "outer.example:443" {
		t.Fatalf("empty inner chain=%q, want outer", got)
	}
	if got := formatTargetChain("outer.example:443", "outer.example:443"); got != "outer.example:443" {
		t.Fatalf("same inner chain=%q, want outer", got)
	}
}
