package proxy

import "testing"

func TestConnectionTrackerCloseUsesUpdatedTarget(t *testing.T) {
	var events []Event
	tracker := newConnectionTracker(Config{
		OnConnection: func(event Event) {
			events = append(events, event)
		},
	}, "127.0.0.1:53000", "203.0.113.10:39281")

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
