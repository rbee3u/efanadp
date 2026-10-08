package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/session"
	"github.com/rbee3u/efanadp/internal/x365"
)

// Logger receives interface status messages; app owns its output resources.
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type noopLogger struct{}

func (noopLogger) Infof(string, ...any)  {}
func (noopLogger) Warnf(string, ...any)  {}
func (noopLogger) Errorf(string, ...any) {}

// Options supplies prepared startup data and the capabilities needed by the UI.
// Prepare validates a selection before the current service is stopped;
// SaveSelection runs on TUI entry and before each service starts.
type Options struct {
	ConfigPath     string
	Listen         string
	Nodes          []x365.Node
	RememberedNode string
	SettingsError  error
	Logger         Logger
	Prepare        func(x365.Node) (session.StartFunc, error)
	SaveSelection  func(string) error
}

// Run owns the terminal. Normal and failed terminal exits stop and await the
// service before process resources close. A second quit explicitly skips waiting.
func Run(opts Options) error {
	return run(opts, func(model *Model) error {
		_, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
		return err
	})
}

func run(opts Options, runProgram func(*Model) error) error {
	if opts.Prepare == nil {
		return fmt.Errorf("missing service preparation")
	}
	model := newModel(opts)
	defer func() {
		model.cancelService()
		if model.service != nil && !model.forceQuit {
			<-model.service.backend.Done()
		}
	}()
	model.saveSelection()
	return runProgram(model)
}

func newModel(opts Options) *Model {
	if opts.Logger == nil {
		opts.Logger = noopLogger{}
	}
	nodes := nodesForDisplay(opts.Nodes)
	model := &Model{
		options:  opts,
		state:    stateNodeList,
		nodeList: newNodeList(nodes, ""),
		nodes:    nodes,
	}
	if opts.SettingsError != nil {
		model.setStatus(statusWarning, "Failed to read settings; using "+opts.ConfigPath+": "+shortError(opts.SettingsError))
		return model
	}
	if remembered := strings.TrimSpace(opts.RememberedNode); remembered != "" {
		model.rememberedNodeName = remembered
		if node, ok := model.selectNodeByName(remembered); ok {
			model.setStatus(statusSuccess, "Restored previous node: "+node.Name)
			model.startupNode = &node
		} else {
			model.setStatus(statusWarning, fmt.Sprintf("Loaded %d nodes; previous node %q is not in this config", len(model.nodes), remembered))
			model.rememberedNodeName = ""
		}
	}
	return model
}

func (m *Model) saveSelection() {
	if m.options.SaveSelection == nil {
		return
	}
	if err := m.options.SaveSelection(m.rememberedNodeName); err != nil {
		if m.canShowSettingsWarning() {
			m.setStatus(statusWarning, "Failed to save settings; next launch may not restore automatically: "+shortError(err))
		}
	}
}
