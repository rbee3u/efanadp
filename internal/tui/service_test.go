package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/x365"
)

func TestStartNodeTransitionsAndStops(t *testing.T) {
	var logs []string
	model := newTestModel(Options{})
	model.options.Logger = testAppLogger{log: func(_ string, format string, args ...any) {
		logs = append(logs, format)
	}}

	cmd := model.requestStartNode(testNode())
	if model.state != stateRunning {
		t.Fatalf("state=%v, want running", model.state)
	}
	if model.service == nil {
		t.Fatal("expected service")
	}
	if model.service.node.Name != "test-node" {
		t.Fatalf("service node=%+v, want test-node", model.service.node)
	}

	model.stopService()
	done := serviceDoneFromStartCmd(t, cmd)
	if done.err != nil {
		t.Fatalf("service returned %v", done.err)
	}

	if len(logs) == 0 {
		t.Fatal("expected service lifecycle logs")
	}
}

type testAppLogger struct {
	log func(level string, format string, args ...any)
}

func (l testAppLogger) Infof(format string, args ...any) {
	l.write("info", format, args...)
}

func (l testAppLogger) Warnf(format string, args ...any) {
	l.write("warn", format, args...)
}

func (l testAppLogger) Errorf(format string, args ...any) {
	l.write("error", format, args...)
}

func (l testAppLogger) write(level string, format string, args ...any) {
	if l.log == nil {
		return
	}
	l.log(level, format, args...)
}

func TestLoadNodesRestoresAndStartsRememberedNode(t *testing.T) {
	var saved string
	model := newTestModel(Options{ConfigPath: "proxy.yaml", Listen: "127.0.0.1:0", Nodes: []x365.Node{testNode()}, RememberedNode: "test-node", SaveSelection: func(name string) error { saved = name; return nil }})

	cmd := model.Init()
	if model.state != stateRunning {
		t.Fatalf("state=%v, want running", model.state)
	}
	if model.service.node.Name != "test-node" {
		t.Fatalf("service node=%+v, want test-node", model.service.node)
	}
	if cmd == nil {
		t.Fatal("expected service command")
	}

	if saved != "test-node" {
		t.Fatalf("saved node=%q", saved)
	}

	model.stopService()
	done := serviceDoneFromStartCmd(t, cmd)
	if done.err != nil {
		t.Fatalf("service returned %v", done.err)
	}
}

func TestSwitchSelectionKeepsCurrentServiceRunning(t *testing.T) {
	model := newTestModel(Options{})

	cmd := model.requestStartNode(testNode())
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	switched := requireModel(t, updated)

	if switched.state != stateNodeList {
		t.Fatalf("state=%v, want nodes", switched.state)
	}
	if switched.service == nil {
		t.Fatal("expected service to keep running while selecting")
	}
	wantHint := "Choose a node, press ↵ to confirm switch, esc to cancel"
	if switched.modeHint != wantHint {
		t.Fatalf("mode hint=%q, want %q", switched.modeHint, wantHint)
	}

	switched.stopService()
	done := serviceDoneFromStartCmd(t, cmd)
	if done.err != nil {
		t.Fatalf("service returned %v", done.err)
	}
	updated, _ = switched.Update(done)
	switched = requireModel(t, updated)
	if switched.modeHint != "" {
		t.Fatalf("mode hint=%q, want cleared after service stop", switched.modeHint)
	}
}

func TestQuitWaitsForRunningServiceToStop(t *testing.T) {
	model := newTestModel(Options{})

	startCmd := model.requestStartNode(testNode())
	updated, quitCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = requireModel(t, updated)
	if quitCmd != nil {
		t.Fatal("quit command returned before service stopped")
	}
	if model.state != stateQuitting {
		t.Fatalf("state=%v, want quitting", model.state)
	}
	if model.service == nil {
		t.Fatal("service cleared before stop message")
	}

	done := serviceDoneFromStartCmd(t, startCmd)
	updated, quitCmd = model.Update(done)
	model = requireModel(t, updated)
	if model.service != nil {
		t.Fatal("service still set after stop during quit")
	}
	requireQuitCommand(t, quitCmd)
}

func TestSecondQuitRequestForcesQuit(t *testing.T) {
	model := newTestModel(Options{})

	startCmd := model.requestStartNode(testNode())
	updated, quitCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = requireModel(t, updated)
	if quitCmd != nil {
		t.Fatal("first quit returned a command before the service stopped")
	}

	_, quitCmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	requireQuitCommand(t, quitCmd)

	// Drain the start command so the fake service goroutine does not outlive the
	// test.
	serviceDoneFromStartCmd(t, startCmd)
}

func TestQuitDiscardsPendingNodeSwitch(t *testing.T) {
	model := newTestModel(Options{})

	startCmd := model.requestStartNode(testNode())
	nextNode := testNode()
	nextNode.Name = "next-node"
	model.requestStartNode(nextNode)
	if model.pendingStart == nil {
		t.Fatal("expected pending node switch")
	}

	updated, quitCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = requireModel(t, updated)
	if quitCmd != nil {
		t.Fatal("quit command returned before switched-from service stopped")
	}
	if model.pendingStart != nil {
		t.Fatal("pending node switch was not discarded on quit")
	}

	done := serviceDoneFromStartCmd(t, startCmd)
	updated, quitCmd = model.Update(done)
	model = requireModel(t, updated)
	if model.service != nil {
		t.Fatal("service restarted from discarded pending node switch")
	}
	requireQuitCommand(t, quitCmd)
}

func TestRunningConfigKeysDoNotChangeState(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateRunning
	model.service = &runningService{id: 7}

	for _, key := range []rune{'c', 'r'} {
		updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		model = requireModel(t, updated)
		if cmd != nil {
			t.Fatalf("key %q returned command; want nil", key)
		}
		if model.state != stateRunning {
			t.Fatalf("key %q state=%v, want running", key, model.state)
		}
	}
}

func TestCancelReturnStatusExpires(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.service = &runningService{id: 7}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = requireModel(t, updated)
	if model.state != stateRunning {
		t.Fatalf("state=%v, want running", model.state)
	}
	if !strings.Contains(model.status, "Cancelled") {
		t.Fatalf("status=%q, want cancel feedback", model.status)
	}
	if model.statusClearAt.IsZero() {
		t.Fatal("expected cancel feedback to have an expiry")
	}

	updated, _ = model.Update(runningTickMsg{serviceID: 7, at: model.statusClearAt.Add(-time.Nanosecond)})
	model = requireModel(t, updated)
	if model.status == "" {
		t.Fatal("status cleared before expiry")
	}

	updated, _ = model.Update(runningTickMsg{serviceID: 7, at: model.statusClearAt})
	model = requireModel(t, updated)
	if model.status != "" {
		t.Fatalf("status=%q, want cleared after expiry", model.status)
	}
}

func requireQuitCommand(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("missing quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("command message type %T, want tea.QuitMsg", msg)
	}
}
