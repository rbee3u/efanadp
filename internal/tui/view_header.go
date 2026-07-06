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
		m.connections.activeCount(),
		m.connections.closed,
		localSOCKS5Link(m.runningListen()),
	)
}

func (m *Model) runningListen() string {
	if m.service != nil && m.service.listen != "" {
		return m.service.listen
	}
	return m.config.listen
}

func (m *Model) runningSummary() string {
	if m.service == nil {
		return "Not running"
	}
	var b strings.Builder
	b.WriteString(m.runningStatus())
	if m.currentNode != nil {
		b.WriteString("\n\n")
		b.WriteString(m.currentNode.Name)
		b.WriteString("\n")
		b.WriteString(m.currentNode.ServerSummary())
	}
	return b.String()
}

func (m *Model) compactRunningSummary() string {
	if m.service == nil {
		return m.runningSummary()
	}
	var b strings.Builder
	b.WriteString(m.runningStatus())
	if m.currentNode != nil {
		b.WriteString("\n\n")
		b.WriteString(m.currentNode.Name)
	}
	return b.String()
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

func (m *Model) transientStatus() string {
	if m.status == "" {
		return ""
	}
	return renderStatus(m.statusKind, m.status)
}

func (m *Model) persistentHint() string {
	if m.modeHint == "" {
		return ""
	}
	if m.modeHintState != m.state {
		return ""
	}
	return renderGuidance(m.modeHint)
}

func (m *Model) noticeBlock() string {
	notices := make([]string, 0, 2)
	if hint := m.persistentHint(); hint != "" {
		notices = append(notices, hint)
	}
	if status := m.transientStatus(); status != "" && m.status != m.modeHint {
		notices = append(notices, status)
	}
	return strings.Join(notices, "\n")
}

// noticeView wraps the notice block to the pane content width so that height
// bookkeeping matches what the pane actually renders for long messages.
func (m *Model) noticeView() string {
	notices := m.noticeBlock()
	if notices == "" {
		return ""
	}
	return lipgloss.NewStyle().Width(m.mainContentWidth()).Render(notices)
}

func renderGuidance(text string) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return ""
	}
	parts := make([]string, len(words))
	for i, word := range words {
		parts[i] = renderGuidanceWord(word)
	}
	return strings.Join(parts, helpStyle.Render(" "))
}

func renderGuidanceWord(word string) string {
	core := strings.Trim(word, ".,;:()[]{}")
	if !isGuidanceKey(core) {
		return helpStyle.Render(word)
	}
	before, after, ok := strings.Cut(word, core)
	if !ok {
		return helpStyle.Render(word)
	}
	return helpStyle.Render(before) +
		helpKeyStyle.Render(core) +
		helpStyle.Render(after)
}

func isGuidanceKey(word string) bool {
	switch strings.ToLower(word) {
	case "enter", "esc", "s", "q", "/", "g/g", "←/→", "↑/↓", "↵":
		return true
	default:
		return false
	}
}

// bannerMinHeight keeps the tall ASCII banner from eating the main pane on
// short terminals; below it the header falls back to the plain brand text.
// bannerGap separates the banner from the summary column, and
// bannerSummaryTopPadding lowers the summary so it sits visually centered in
// the banner's top half, and bannerMinSummaryWidth keeps it readable.
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
		return mutedStyle.Width(width).Render(m.runningSummary())
	}
	summary := m.runningSummary()
	if width < 72 {
		summary = m.compactRunningSummary()
	}
	lines := strings.Split(summary, "\n")
	if len(lines) > 0 {
		lines[0] = statusSuccessStyle.Render(lines[0])
	}
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
