package tui

import "strings"

func (m *Model) runningBody() string {
	var b strings.Builder
	activeConnections, total := m.connections.pageItems(m.connections.page)
	writeConnectionRows(&b, activeConnections, m.connections.now, m.mainContentWidth(), m.connections.page, m.connections.size(), total)
	return b.String()
}

func (m *Model) mainBody() string {
	switch m.state {
	case stateNodeList:
		// Clamp to the budgeted height: the bubbles list can render its own
		// minimum layout that exceeds a small SetSize, which would push the
		// window past the terminal height and scroll the top border off.
		return clampHeight(m.nodeList.View(), m.nodeListHeight())
	case stateRunning:
		return m.runningBody()
	case stateQuitting:
		return ""
	default:
		return ""
	}
}
