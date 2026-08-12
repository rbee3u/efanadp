package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
	"github.com/rbee3u/efanadp/internal/x365"

	tea "github.com/charmbracelet/bubbletea"
)

type serveSOCKSFunc func(context.Context, string, proxy.Config) error

type runningService struct {
	id               int
	cancel           context.CancelFunc
	node             x365.Node
	listen           string
	connectionEvents *connectionQueue
	eventsDone       <-chan struct{}
}

type pendingStart struct {
	node   x365.Node
	userID x365.UserID
}

func (m *Model) requestStartNode(node x365.Node, stoppingStatus string) tea.Cmd {
	userID, err := node.UserID()
	if err != nil {
		m.setStatus(statusError, fmt.Sprintf("Invalid node user ID: %v", err))
		m.logErrorf("[efanadp] invalid node user id node=%s err=%v", node.Name, err)
		return nil
	}
	start := pendingStart{node: node, userID: userID}
	if m.service != nil {
		m.pendingStart = &start
		m.stopService()
		if stoppingStatus == "" {
			stoppingStatus = "Stopping current service, then starting node: " + node.Name
		}
		m.clearModeHint()
		m.setStatus(statusInfo, stoppingStatus)
		return nil
	}
	return m.startService(start)
}

func (m *Model) startService(start pendingStart) tea.Cmd {
	// A SOCKS service is an app-level task owned by the TUI model, not by a
	// single Bubble Tea update message, so it starts from a fresh root context.
	ctx, cancel := context.WithCancel(context.Background())
	id := m.nextServiceID
	m.nextServiceID++
	listen := m.config.loaded.Listen
	done := make(chan serviceStoppedMsg, 1)
	events := newConnectionQueue()
	stopped := make(chan struct{})
	service := &runningService{
		id:               id,
		cancel:           cancel,
		node:             start.node,
		listen:           listen,
		connectionEvents: events,
		eventsDone:       stopped,
	}
	cfg := m.serverConfig(start.node, start.userID, events)

	m.setRunningService(service)
	m.logServiceStart(start.node, service.listen)
	serveSOCKS := m.deps.serveSOCKS
	go func() {
		err := serveSOCKS(ctx, service.listen, cfg)
		// The service goroutine owns stopped/done. connectionEvents has no
		// close: connection handlers may emit after the listener stops, and
		// pushing to the queue stays safe for the whole process lifetime.
		close(stopped)
		done <- serviceStoppedMsg{serviceID: service.id, nodeName: service.node.Name, err: err}
	}()

	return tea.Batch(
		waitServiceStopped(done),
		waitConnectionEvent(id, service.connectionEvents, stopped),
		runningTick(id),
	)
}

func (m *Model) serverConfig(node x365.Node, userID x365.UserID, events *connectionQueue) proxy.Config {
	upstream := x365.NewClient(node, userID, m.config.timeout)
	return proxy.Config{
		Upstream:                upstream,
		HandshakeTimeout:        m.config.handshakeTimeout,
		ShadowsocksSniffConfigs: m.config.loaded.ShadowsocksSniffConfigs,
		Logger:                  m.deps.logger,
		OnConnection:            events.push,
	}
}

func (m *Model) setRunningService(service *runningService) {
	m.service = service
	m.rememberedNodeName = service.node.Name
	m.connections.reset(time.Now())
	m.state = stateRunning
	m.clearModeHint()
	// The header already shows the live running summary; a transient copy
	// would go stale as soon as the connection counters change.
	m.clearStatus()
	m.syncConnectionPageSize()
	m.saveSettingsLogged()
}

func (m *Model) logServiceStart(node x365.Node, listen string) {
	m.logInfof("[efanadp] listening: %s", listen)
	m.logInfof("[efanadp] node: %s", node.Label())
	m.logInfof("[efanadp] transport: xhttp-reality")
}

func (m *Model) handleServiceStopped(msg serviceStoppedMsg) (tea.Model, tea.Cmd) {
	if m.service == nil || msg.serviceID != m.service.id {
		return m, nil
	}
	if m.state == stateQuitting {
		m.clearStoppedService()
		if msg.err != nil {
			m.logErrorf("[efanadp] service exited during quit node=%s err=%v", msg.nodeName, msg.err)
		} else {
			m.logInfof("[efanadp] service stopped node=%s", msg.nodeName)
		}
		return m, tea.Quit
	}
	pending := m.clearStoppedService()
	if pending != nil {
		m.logInfof("[efanadp] service stopped node=%s; starting next node=%s", msg.nodeName, pending.node.Name)
		cmd := m.startService(*pending)
		return m, cmd
	}
	if msg.err != nil {
		m.state = stateNodeList
		m.clearModeHint()
		m.setStatus(statusError, fmt.Sprintf("Service exited: %v", msg.err))
		m.logErrorf("[efanadp] service exited node=%s err=%v", msg.nodeName, msg.err)
		return m, nil
	}
	if m.state == stateRunning {
		m.state = stateNodeList
	}
	m.clearModeHint()
	m.setStatus(statusSuccess, "Service stopped: "+msg.nodeName)
	m.logInfof("[efanadp] service stopped node=%s", msg.nodeName)
	return m, nil
}

func (m *Model) clearStoppedService() *pendingStart {
	pending := m.pendingStart
	m.pendingStart = nil
	m.service = nil
	return pending
}

func (m *Model) stopService() {
	if m.service == nil {
		return
	}
	m.logInfof("[efanadp] stopping node: %s", m.service.node.Name)
	m.service.cancel()
}

func waitServiceStopped(done <-chan serviceStoppedMsg) tea.Cmd {
	return func() tea.Msg {
		return <-done
	}
}

// waitConnectionEvent waits for queued connection events and delivers the whole
// backlog in one message, so a burst costs one render instead of one per event.
// It returns nil once the service has stopped and drained, which ends the
// re-arm loop in handleGlobalMsg.
func waitConnectionEvent(serviceID int, events *connectionQueue, stopped <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		for {
			if batch := events.take(); batch != nil {
				return connectionEventMsg{serviceID: serviceID, events: batch}
			}
			select {
			case <-events.signal:
			case <-stopped:
				// Late events from handlers outliving the listener are still
				// worth applying; only stop once nothing is left.
				return takeFinalConnectionEvents(serviceID, events)
			}
		}
	}
}

func takeFinalConnectionEvents(serviceID int, events *connectionQueue) tea.Msg {
	if batch := events.take(); batch != nil {
		return connectionEventMsg{serviceID: serviceID, events: batch}
	}
	return nil
}

func runningTick(serviceID int) tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return runningTickMsg{serviceID: serviceID, at: t}
	})
}
