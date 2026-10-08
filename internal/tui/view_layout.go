package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func fillMainContent(body, notices, help string, height int) string {
	body = strings.TrimRight(body, "\n")
	tail := help
	tailHeight := lipgloss.Height(help)
	if notices != "" {
		tail = notices + "\n\n" + help
		tailHeight += lipgloss.Height(notices) + 1
	}
	gap := height - lipgloss.Height(body) - tailHeight + 1
	gap = max(gap, 2)
	return body + strings.Repeat("\n", gap) + tail
}

func clampHeight(s string, height int) string {
	if height < 1 {
		height = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

// Style.Width includes padding but excludes borders; subtracting the full frame
// would shrink and wrap content.
func renderAppWindow(width, paneWidth int, header, main string) string {
	headerPane := headerPaneStyle.Width(max(1, paneWidth-headerPaneStyle.GetHorizontalBorderSize())).Render(header)
	mainPane := mainPaneStyle.Width(max(1, paneWidth-mainPaneStyle.GetHorizontalBorderSize())).Render(main)
	content := lipgloss.JoinVertical(lipgloss.Left, headerPane, mainPane)
	return appWindowStyle.Width(max(1, width-appWindowStyle.GetHorizontalBorderSize())).Render(content)
}

func (m *Model) nodeListHeight() int {
	height := m.mainContentHeight()
	if notices := m.noticeView(); notices != "" {
		height -= lipgloss.Height(notices) + 1
	}
	height -= lipgloss.Height(renderHelp(m.nodeHelp(), m.mainContentWidth())) + 1
	return max(8, height)
}

func (m *Model) syncConnectionPageSize() {
	m.connections.setPageSize(m.connectionRowsPerPage())
}

// runningNoticeReserve holds two lines above the help footer for transient
// status messages so the connection table never moves when one appears.
const runningNoticeReserve = 2

// Reserve four lines for the table header, page summary, and gap.
func (m *Model) connectionRowsPerPage() int {
	height := m.mainContentHeight()
	helpHeight := lipgloss.Height(renderHelp(runningHelp(), m.mainContentWidth()))
	return max(1, height-4-runningNoticeReserve-helpHeight)
}

func (m *Model) windowWidth() int {
	if m.width <= 0 {
		return 160
	}
	return max(30, m.width)
}

func (m *Model) paneWidth() int {
	return max(20, m.windowWidth()-appWindowStyle.GetHorizontalFrameSize())
}

func (m *Model) mainContentWidth() int {
	return max(20, m.paneWidth()-mainPaneStyle.GetHorizontalFrameSize())
}

func (m *Model) mainContentHeight() int {
	if m.height <= 0 {
		return 18
	}
	height := m.height - appWindowStyle.GetVerticalFrameSize()
	headerWidth := max(1, m.paneWidth()-headerPaneStyle.GetHorizontalBorderSize())
	height -= lipgloss.Height(headerPaneStyle.Width(headerWidth).Render(m.windowHeader()))
	height -= mainPaneStyle.GetVerticalFrameSize()
	return max(8, height)
}
