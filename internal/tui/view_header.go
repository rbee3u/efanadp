package tui

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) runningStatus() string {
	return fmt.Sprintf(
		"Running · Active: %d · Closed: %d · URL: %s",
		len(m.connections.active),
		m.connections.closed,
		localSOCKS5Link(m.options.Listen),
	)
}

func localSOCKS5Link(listen string) string {
	link := url.URL{
		Scheme:   "socks5",
		Host:     localSOCKS5LinkHost(listen),
		Fragment: "efanadp",
	}
	return link.String()
}

func localSOCKS5LinkHost(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return strings.TrimSpace(listen)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func (m *Model) noticeBlock() string {
	notices := make([]string, 0, 2)
	if m.modeHintState == m.state {
		if hint := renderGuidance(m.modeHint); hint != "" {
			notices = append(notices, hint)
		}
	}
	if m.status != "" && m.status != m.modeHint {
		if status := statusStyle(m.statusKind).Render(m.status); status != "" {
			notices = append(notices, status)
		}
	}
	return strings.Join(notices, "\n")
}

// Wrap before measuring height so long notices fit the pane.
func (m *Model) noticeView() string {
	notices := m.noticeBlock()
	if notices == "" {
		return ""
	}
	return lipgloss.NewStyle().Width(m.mainContentWidth()).Render(notices)
}

// Hide the banner when it would crowd the main pane or summary.
const (
	bannerMinHeight         = 24
	bannerGap               = 3
	bannerSummaryTopPadding = 1
	bannerMinSummaryWidth   = 40
)

func (m *Model) windowHeader() string {
	contentWidth := m.mainContentWidth()
	if m.showBanner(contentWidth) {
		banner := appBrandStyle.Render(appBanner)
		summaryWidth := contentWidth - lipgloss.Width(appBanner) - bannerGap
		summary := m.renderRunningSummary(summaryWidth)
		summary = lipgloss.NewStyle().PaddingTop(bannerSummaryTopPadding).Render(summary)
		gap := strings.Repeat(" ", bannerGap)
		return lipgloss.JoinHorizontal(lipgloss.Top, banner, gap, summary)
	}
	brand := appBrandStyle.Width(contentWidth).Render("efanadp")
	summary := m.renderRunningSummary(contentWidth)
	return lipgloss.JoinVertical(lipgloss.Left, brand, summary)
}

func (m *Model) showBanner(contentWidth int) bool {
	return m.height >= bannerMinHeight &&
		contentWidth >= lipgloss.Width(appBanner)+bannerGap+bannerMinSummaryWidth
}

func (m *Model) renderRunningSummary(width int) string {
	if m.service == nil {
		return mutedStyle.Width(width).Render("Not running")
	}
	summary := m.runningStatus()
	if node := m.currentNode(); node != nil {
		summary += "\n\n" + node.Name
		if width >= 72 {
			summary += "\n" + node.ServerSummary()
		}
	}
	lines := strings.Split(summary, "\n")
	lines[0] = statusSuccessStyle.Render(lines[0])
	styledNodeTitle := false
	for i := 1; i < len(lines); i++ {
		if lines[i] == "" {
			continue
		}
		if !styledNodeTitle {
			lines[i] = nodeTitleStyle.Render(lines[i])
			styledNodeTitle = true
			continue
		}
		lines[i] = nodeDetailStyle.Render(lines[i])
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

const appBanner = `███████╗███████╗ █████╗ ███╗   ██╗ █████╗ ██████╗ ██████╗
██╔════╝██╔════╝██╔══██╗████╗  ██║██╔══██╗██╔══██╗██╔══██╗
█████╗  █████╗  ███████║██╔██╗ ██║███████║██║  ██║██████╔╝
██╔══╝  ██╔══╝  ██╔══██║██║╚██╗██║██╔══██║██║  ██║██╔═══╝
███████╗██║     ██║  ██║██║ ╚████║██║  ██║██████╔╝██║
╚══════╝╚═╝     ╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝╚═════╝ ╚═╝`
