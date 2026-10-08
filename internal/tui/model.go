// Package tui implements terminal interaction and the session connection projection.
// Process resources and service execution are supplied by the application.
package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/x365"
)

type viewState int

const (
	stateNodeList viewState = iota
	stateRunning
	stateQuitting
)

// Model is the Bubble Tea model for the whole interface. Its methods run on the
// single event loop goroutine and must not block; background work belongs in a
// tea.Cmd that reports back as a message.
type Model struct {
	options Options

	state    viewState
	nodeList list.Model
	nodes    []x365.Node

	width         int
	height        int
	status        string
	statusKind    statusKind
	statusClearAt time.Time
	modeHint      string
	modeHintState viewState

	service       *runningService
	pendingStart  *pendingStart
	nextServiceID int
	forceQuit     bool

	connections connectionState

	rememberedNodeName string
	startupNode        *x365.Node
}

func (m *Model) Init() tea.Cmd {
	if m.startupNode == nil {
		return nil
	}
	node := *m.startupNode
	m.startupNode = nil
	return m.requestStartNode(node)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeNodeList()
		m.syncConnectionPageSize()
		return m, nil

	case serviceStoppedMsg:
		cmd := m.handleServiceStopped(msg)
		return m, cmd

	case connectionEventsMsg:
		if m.service == nil || msg.serviceID != m.service.id {
			return m, nil
		}
		for _, event := range msg.events {
			m.connections.apply(event)
		}
		return m, waitConnectionEvents(msg.serviceID, m.service.backend)

	case runningTickMsg:
		if m.service == nil || msg.serviceID != m.service.id {
			return m, nil
		}
		m.connections.now = msg.at
		m.clearExpiredStatus(msg.at)
		return m, runningTick(msg.serviceID)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if msg.String() == "q" && m.state == stateNodeList && m.nodeList.SettingFilter() {
				break
			}
			cmd := m.requestQuit()
			return m, cmd
		}
	}

	switch m.state {
	case stateNodeList:
		cmd := m.updateNodes(msg)
		return m, cmd
	case stateRunning:
		m.updateRunning(msg)
		return m, nil
	case stateQuitting:
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) updateRunning(msg tea.Msg) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return
	}

	switch key.String() {
	case "s":
		m.state = stateNodeList
		m.clearStatus()
		m.setModeHint("Choose a node, press ↵ to confirm switch, esc to cancel")
		m.prepareNodeSelection()
	case "right", "pgdown":
		m.connections.nextPage()
	case "left", "pgup":
		m.connections.previousPage()
	}
}

func (m *Model) requestQuit() tea.Cmd {
	m.pendingStart = nil
	if m.service == nil {
		return tea.Quit
	}
	if m.state == stateQuitting {
		// A second quit abandons stuck teardown; process exit reclaims the
		// service.
		m.forceQuit = true
		m.options.Logger.Warnf("[efanadp] quit requested again node=%s; abandoning service shutdown", m.service.node.Name)
		return tea.Quit
	}
	m.state = stateQuitting
	m.clearModeHint()
	m.setStatus(statusInfo, "Stopping service before quit: "+m.service.node.Name+" (press again to force quit)")
	m.stopService()
	return nil
}
