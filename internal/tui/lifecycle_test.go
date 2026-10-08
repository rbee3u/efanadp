package tui

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
	"github.com/rbee3u/efanadp/internal/session"
	"github.com/rbee3u/efanadp/internal/x365"
)

func TestRejectedSelectionKeepsCurrentSession(t *testing.T) {
	model := newTestModel(Options{})
	model.requestStartNode(testNode())
	original := model.service
	model.options.Prepare = func(x365.Node) (session.StartFunc, error) { return nil, errors.New("invalid selection") }
	if cmd := model.requestStartNode(testNode()); cmd != nil {
		t.Fatal("rejected selection returned a command")
	}
	if model.service != original || model.pendingStart != nil {
		t.Fatal("rejected selection replaced the current session")
	}
	select {
	case <-original.backend.Done():
		t.Fatal("current session stopped")
	default:
	}
	model.stopService()
}

func TestSwitchStartsOnlyAfterCurrentCompletionAndIgnoresOldMessages(t *testing.T) {
	var starts int
	model := newTestModel(Options{Prepare: func(x365.Node) (session.StartFunc, error) {
		return func() session.Service { starts++; return newFakeService() }, nil
	}})
	firstCmd := model.requestStartNode(testNode())
	oldID := model.service.id
	next := testNode()
	next.Name = "next-node"
	model.requestStartNode(next)
	if starts != 1 {
		t.Fatal("next service started before completion was handled")
	}
	model.Update(serviceDoneFromStartCmd(t, firstCmd))
	if starts != 2 || model.service.node.Name != next.Name {
		t.Fatal("next service did not start after completion")
	}
	current := model.service
	model.Update(connectionEventsMsg{serviceID: oldID, events: []proxy.Event{{ID: 1, Type: proxy.ConnectionOpened, Stats: &proxy.Stats{}}}})
	model.Update(serviceStoppedMsg{serviceID: oldID, err: errors.New("late error")})
	if model.service != current || len(model.connections.active) != 0 {
		t.Fatal("old session messages changed the new session")
	}
	model.stopService()
}

// A stopped service may still be releasing handlers. Terminal failure must
// await that completion before app closes its logger; forced quit must not.
type gatedService struct {
	*fakeService
	stopped  chan struct{}
	stopOnce sync.Once
}

func (s *gatedService) Stop() { s.stopOnce.Do(func() { close(s.stopped) }) }

func TestTerminalExitOwnsServiceCleanup(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "failure"
		if force {
			name = "forced quit"
		}
		t.Run(name, func(t *testing.T) {
			service := &gatedService{fakeService: newFakeService(), stopped: make(chan struct{})}
			boom := errors.New("terminal failed")
			result := make(chan error, 1)
			go func() {
				result <- run(Options{Prepare: func(x365.Node) (session.StartFunc, error) { return func() session.Service { return service }, nil }}, func(model *Model) error {
					model.requestStartNode(testNode())
					if force {
						model.requestQuit()
						model.requestQuit()
					}
					return boom
				})
			}()
			select {
			case <-service.stopped:
			case <-time.After(time.Second):
				t.Fatal("service cancellation missing")
			}
			if !force {
				select {
				case <-result:
					t.Fatal("returned before service cleanup")
				default:
				}
				service.fakeService.Stop()
			}
			select {
			case err := <-result:
				if !errors.Is(err, boom) {
					t.Fatalf("error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("terminal exit blocked")
			}
			service.fakeService.Stop()
		})
	}
}
