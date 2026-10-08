package tui

func (m *Model) View() string {
	view := renderAppWindow(m.windowWidth(), m.paneWidth(), m.windowHeader(), m.mainView())
	if m.height > 0 {
		// Clip the bottom to prevent scrolling the top border off-screen.
		view = clampHeight(view, m.height)
	}
	return view
}

func (m *Model) mainView() string {
	height := m.mainContentHeight()
	help := renderHelp(m.helpItems(), m.mainContentWidth())
	body := m.mainBody()
	return fillMainContent(body, m.noticeView(), help, height)
}

func (m *Model) mainBody() string {
	switch m.state {
	case stateNodeList:
		// The bubbles list's minimum layout can exceed SetSize on short terminals.
		return clampHeight(m.nodeList.View(), m.nodeListHeight())
	case stateRunning:
		return m.runningBody()
	case stateQuitting:
		return ""
	default:
		return ""
	}
}

func (m *Model) runningBody() string {
	rows, total := m.connections.pageItems()
	return renderConnectionTable(rows, m.connections.now, m.mainContentWidth(), m.connections.page, m.connections.rowsPerPage(), total)
}
