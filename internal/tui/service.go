package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/proxy"
	"github.com/rbee3u/efanadp/internal/session"
	"github.com/rbee3u/efanadp/internal/x365"
)

type serviceStoppedMsg struct {
	serviceID int
	nodeName  string
	err       error
}

// Batch events to avoid rendering once per event.
type connectionEventsMsg struct {
	serviceID int
	events    []proxy.Event
}

type runningTickMsg struct {
	serviceID int
	at        time.Time
}

type runningService struct {
	id      int
	node    x365.Node
	backend session.Service
}

type pendingStart struct {
	node  x365.Node
	start session.StartFunc
}

func (m *Model) requestStartNode(node x365.Node) tea.Cmd {
	startService, err := m.options.Prepare(node)
	if err != nil {
		m.setStatus(statusError, fmt.Sprintf("Invalid node user ID: %v", err))
		m.options.Logger.Errorf("[efanadp] invalid node user id node=%s err=%v", node.Name, err)
		return nil
	}
	start := pendingStart{node: node, start: startService}
	if m.service != nil {
		m.pendingStart = &start
		m.stopService()
		m.clearModeHint()
		m.setStatus(statusInfo, "Stopping current service, then starting node: "+node.Name)
		return nil
	}
	return m.startService(start)
}

func (m *Model) startService(start pendingStart) tea.Cmd {
	id := m.nextServiceID
	m.nextServiceID++
	service := &runningService{id: id, node: start.node}
	m.service = service
	m.rememberedNodeName = service.node.Name
	m.connections.reset(time.Now())
	m.state = stateRunning
	m.clearModeHint()
	// The header owns the live summary; a status copy would go stale.
	m.clearStatus()
	m.syncConnectionPageSize()
	m.saveSelection()

	m.options.Logger.Infof("[efanadp] listening: %s", m.options.Listen)
	m.options.Logger.Infof("[efanadp] node: %s", start.node.Label())
	m.options.Logger.Infof("[efanadp] transport: xhttp-reality")
	service.backend = start.start()
	return tea.Batch(waitServiceStopped(service), waitConnectionEvents(id, service.backend), runningTick(id))
}

func (m *Model) handleServiceStopped(msg serviceStoppedMsg) tea.Cmd {
	if m.service == nil || msg.serviceID != m.service.id {
		return nil
	}
	pending := m.pendingStart
	m.pendingStart = nil
	m.service = nil
	if m.state == stateQuitting {
		if msg.err != nil {
			m.options.Logger.Errorf("[efanadp] service exited during quit node=%s err=%v", msg.nodeName, msg.err)
		} else {
			m.options.Logger.Infof("[efanadp] service stopped node=%s", msg.nodeName)
		}
		return tea.Quit
	}
	if pending != nil {
		m.options.Logger.Infof("[efanadp] service stopped node=%s; starting next node=%s", msg.nodeName, pending.node.Name)
		return m.startService(*pending)
	}
	if msg.err != nil {
		m.state = stateNodeList
		m.clearModeHint()
		m.setStatus(statusError, fmt.Sprintf("Service exited: %v", msg.err))
		m.options.Logger.Errorf("[efanadp] service exited node=%s err=%v", msg.nodeName, msg.err)
		return nil
	}
	if m.state == stateRunning {
		m.state = stateNodeList
	}
	m.clearModeHint()
	m.setStatus(statusSuccess, "Service stopped: "+msg.nodeName)
	m.options.Logger.Infof("[efanadp] service stopped node=%s", msg.nodeName)
	return nil
}

func (m *Model) stopService() {
	if m.service == nil {
		return
	}
	m.options.Logger.Infof("[efanadp] stopping node: %s", m.service.node.Name)
	m.cancelService()
}

func (m *Model) cancelService() {
	if m.service != nil && m.service.backend != nil {
		m.service.backend.Stop()
	}
}

func waitServiceStopped(service *runningService) tea.Cmd {
	return func() tea.Msg {
		<-service.backend.Done()
		return serviceStoppedMsg{serviceID: service.id, nodeName: service.node.Name, err: service.backend.Err()}
	}
}

func waitConnectionEvents(serviceID int, service session.Service) tea.Cmd {
	return func() tea.Msg {
		if batch := service.NextEvents(); batch != nil {
			return connectionEventsMsg{serviceID: serviceID, events: batch}
		}
		return nil
	}
}

func runningTick(serviceID int) tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return runningTickMsg{serviceID: serviceID, at: t}
	})
}

func (m *Model) currentNodeName() string {
	node := m.currentNode()
	if node == nil {
		return ""
	}
	return node.Name
}

func (m *Model) currentNode() *x365.Node {
	if m.service == nil || m.service.node.Name == "" {
		return nil
	}
	return &m.service.node
}
