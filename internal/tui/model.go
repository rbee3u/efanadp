package tui

import (
	"time"

	"github.com/rbee3u/efanadp/internal/x365"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	config runtimeConfig
	deps   modelDeps

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
	return m.requestStartNode(node, "")
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleGlobalMsg(msg); ok {
		return next, cmd
	}

	switch m.state {
	case stateNodeList:
		return m.updateNodes(msg)
	case stateRunning:
		return m.updateRunning(msg)
	case stateQuitting:
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) handleGlobalMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeNodeList()
		m.syncConnectionPageSize()
		return m, nil, true

	case serviceStoppedMsg:
		next, cmd := m.handleServiceStopped(msg)
		return next, cmd, true

	case connectionEventMsg:
		if m.service == nil || msg.serviceID != m.service.id {
			return m, nil, true
		}
		m.connections.apply(msg.event)
		return m, waitConnectionEvent(msg.serviceID, m.service.connectionEvents, m.service.eventsDone), true

	case runningTickMsg:
		if m.service == nil || msg.serviceID != m.service.id {
			return m, nil, true
		}
		m.connections.now = msg.at
		m.clearExpiredStatus(msg.at)
		return m, runningTick(msg.serviceID), true

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			cmd := m.requestQuit()
			return m, cmd, true
		case "q":
			if m.state == stateNodeList && m.nodeList.SettingFilter() {
				break
			}
			cmd := m.requestQuit()
			return m, cmd, true
		}
	}
	return m, nil, false
}

func (m *Model) requestQuit() tea.Cmd {
	m.pendingStart = nil
	if m.service == nil {
		return tea.Quit
	}
	if m.state != stateQuitting {
		m.state = stateQuitting
		m.clearModeHint()
		m.setStatus(statusInfo, "Stopping service before quit: "+m.service.node.Name)
		m.stopService()
	}
	return nil
}
