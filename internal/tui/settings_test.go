package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/rbee3u/efanadp/internal/x365"
)

func TestSaveSettingsFailureWarningRespectsVisibleStatus(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(m *Model)
		wantWarning bool
		wantStatus  string
	}{
		{
			name:        "no status shows the warning",
			setup:       func(*Model) {},
			wantWarning: true,
		},
		{
			name:        "benign status gives way",
			setup:       func(m *Model) { m.setStatus(statusSuccess, "Loaded config") },
			wantWarning: true,
		},
		{
			name: "cleared error shows the warning",
			setup: func(m *Model) {
				m.setStatus(statusError, "Service exited")
				m.clearStatus()
			},
			wantWarning: true,
		},
		{
			name:       "visible error is kept",
			setup:      func(m *Model) { m.setStatus(statusError, "Service exited: boom") },
			wantStatus: "Service exited: boom",
		},
		{
			name:       "visible warning is kept",
			setup:      func(m *Model) { m.setStatus(statusWarning, "Node switch failed") },
			wantStatus: "Node switch failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newTestModel(Options{})
			model.options.SaveSelection = func(string) error { return errors.New("write failed") }
			model.options.ConfigPath = "proxy.yaml"
			model.rememberedNodeName = "test-node"
			tt.setup(model)

			model.saveSelection()

			if !tt.wantWarning {
				if model.status != tt.wantStatus {
					t.Fatalf("status=%q, want the visible message kept: %q", model.status, tt.wantStatus)
				}
				return
			}
			if model.statusKind != statusWarning {
				t.Fatalf("status kind=%v, want warning", model.statusKind)
			}
			if !strings.Contains(model.status, "Failed to save settings") ||
				!strings.Contains(model.status, "may not restore automatically") {
				t.Fatalf("status=%q, want settings save warning", model.status)
			}
		})
	}
}

func TestNewModelRestoresSettingsNode(t *testing.T) {
	model := newTestModel(Options{ConfigPath: "proxy.yaml", Nodes: []x365.Node{testNode()}, RememberedNode: "test-node"})

	if got := model.rememberedNodeName; got != "test-node" {
		t.Fatalf("remembered node=%q, want test-node", got)
	}
	if model.startupNode == nil || model.startupNode.Name != "test-node" {
		t.Fatalf("startup node=%+v, want test-node", model.startupNode)
	}
}
