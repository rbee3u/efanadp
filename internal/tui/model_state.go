package tui

import (
	"time"

	"github.com/rbee3u/efanadp/internal/x365"
)

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

func (m *Model) resizeNodeList() {
	m.reserveNodePaginationHeight()
	m.nodeList.SetSize(m.mainContentWidth(), m.nodeListHeight())
}

func (m *Model) reserveNodePaginationHeight() {
	if len(m.nodeList.VisibleItems()) > 1 && m.nodeList.Paginator.TotalPages < 2 {
		m.nodeList.Paginator.TotalPages = 2
	}
}

func (m *Model) logInfof(format string, args ...any) {
	m.deps.logger.Infof(format, args...)
}

func (m *Model) logWarnf(format string, args ...any) {
	m.deps.logger.Warnf(format, args...)
}

func (m *Model) logErrorf(format string, args ...any) {
	m.deps.logger.Errorf(format, args...)
}

func (m *Model) setStatus(kind statusKind, text string) {
	m.statusKind = kind
	m.status = text
	m.statusClearAt = time.Time{}
}

func (m *Model) clearStatus() {
	m.status = ""
	m.statusClearAt = time.Time{}
}

func (m *Model) setExpiringStatus(kind statusKind, text string, ttl time.Duration) {
	m.setStatus(kind, text)
	m.statusClearAt = time.Now().Add(ttl)
}

func (m *Model) clearExpiredStatus(now time.Time) {
	if !m.statusClearAt.IsZero() && !now.Before(m.statusClearAt) {
		m.clearStatus()
	}
}

func (m *Model) setModeHint(text string) {
	m.modeHint = text
	m.modeHintState = m.state
}

func (m *Model) clearModeHint() {
	m.modeHint = ""
}

func (m *Model) returnToRunning() bool {
	if m.service == nil {
		return false
	}
	m.state = stateRunning
	m.clearModeHint()
	m.setExpiringStatus(statusInfo, "Cancelled; current service keeps running", 2*time.Second)
	return true
}
