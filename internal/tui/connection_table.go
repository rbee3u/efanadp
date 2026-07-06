package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rbee3u/efanadp/internal/human"

	"github.com/charmbracelet/lipgloss"
)

func writeConnectionRows(b *strings.Builder, rows []connectionItem, now time.Time, width, page, pageSize, total int) {
	layout := newConnectionTableLayout(rows, now, width)
	b.WriteString(renderConnectionTableHeader(layout))

	if len(rows) == 0 {
		b.WriteString(mutedStyle.Render("No active connections; waiting for mihomo or curl to use the local SOCKS5 proxy"))
		b.WriteString("\n")
	} else {
		for _, conn := range rows {
			b.WriteString(renderConnectionRow(conn, now, layout))
		}
	}
	// When there are multiple pages, pad a partial last page up to a full page
	// so the pagination dots stay pinned to the same row instead of hugging the
	// last connection and jumping as pages fill up (matching the node list).
	if pageCount(total, pageSize) >= 2 {
		for i := len(rows); i < pageSize; i++ {
			b.WriteString("\n")
		}
	}
	b.WriteString(renderConnectionPageSummary(page, pageSize, total))
}

func (c connectionItem) uploadBytes() int64 {
	if c.stats == nil {
		return 0
	}
	return c.stats.Upload()
}

func (c connectionItem) downloadBytes() int64 {
	if c.stats == nil {
		return 0
	}
	return c.stats.Download()
}

func formatConnectionAge(now, startedAt time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if startedAt.IsZero() {
		return "0s"
	}
	d := now.Sub(startedAt)
	d = max(d, 0)
	return shortHumanDuration(d)
}

func shortHumanDuration(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		minutes := int(d / time.Minute)
		seconds := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}

type connectionTableLayout struct {
	totalWidth int
	idWidth    int
	typeWidth  int
	fromWidth  int
	toWidth    int
	ageWidth   int
	upWidth    int
	downWidth  int
}

func newConnectionTableLayout(rows []connectionItem, now time.Time, width int) connectionTableLayout {
	const maxTargetWidth = 80

	availableWidth := width
	if availableWidth <= 0 {
		availableWidth = 118
	}
	availableWidth = max(58, availableWidth)

	layout := connectionTableLayout{
		idWidth:   lipgloss.Width("ID"),
		typeWidth: lipgloss.Width("Type"),
		ageWidth:  lipgloss.Width("Age"),
		upWidth:   lipgloss.Width("Up"),
		downWidth: lipgloss.Width("Down"),
	}
	for _, conn := range rows {
		layout.idWidth = max(layout.idWidth, lipgloss.Width(fmt.Sprintf("#%d", conn.id)))
		layout.typeWidth = max(layout.typeWidth, lipgloss.Width(conn.protocolLabel()))
		layout.ageWidth = max(layout.ageWidth, lipgloss.Width(formatConnectionAge(now, conn.startedAt)))
		layout.upWidth = max(layout.upWidth, lipgloss.Width(human.FormatBytes(conn.uploadBytes())))
		layout.downWidth = max(layout.downWidth, lipgloss.Width(human.FormatBytes(conn.downloadBytes())))
	}

	layout.idWidth = min(layout.idWidth, 6)
	layout.typeWidth = min(layout.typeWidth, 4)
	layout.ageWidth = min(layout.ageWidth, 6)
	layout.upWidth = min(layout.upWidth, 9)
	layout.downWidth = min(layout.downWidth, 9)

	fixedWidth := layout.idWidth + layout.typeWidth + layout.ageWidth + layout.upWidth + layout.downWidth + 12
	peerWidth := max(20, availableWidth-fixedWidth)
	layout.fromWidth = min(22, max(10, peerWidth/3))
	layout.toWidth = min(maxTargetWidth, max(10, peerWidth-layout.fromWidth))
	layout.totalWidth = layout.idWidth +
		layout.typeWidth +
		layout.fromWidth +
		layout.toWidth +
		layout.ageWidth +
		layout.upWidth +
		layout.downWidth +
		12
	return layout
}

func (c connectionItem) protocolLabel() string {
	if c.protocol != "" {
		return c.protocol
	}
	return "tcp"
}

func renderConnectionTableHeader(layout connectionTableLayout) string {
	columns := []string{
		tableHeaderStyle.Render(formatConnectionCell("ID", layout.idWidth, false)),
		tableHeaderStyle.Render(formatConnectionCell("Type", layout.typeWidth, false)),
		tableHeaderStyle.Render(formatConnectionCell("Source", layout.fromWidth, false)),
		tableHeaderStyle.Render(formatConnectionCell("Target", layout.toWidth, false)),
		tableHeaderStyle.Render(formatConnectionCell("Age", layout.ageWidth, true)),
		tableHeaderStyle.Render(formatConnectionCell("Up", layout.upWidth, true)),
		tableHeaderStyle.Render(formatConnectionCell("Down", layout.downWidth, true)),
	}
	return strings.Join(columns, "  ") + "\n" +
		tableRuleStyle.Render(strings.Repeat("-", layout.totalWidth)) + "\n"
}

func renderConnectionPageSummary(page, pageSize, total int) string {
	dots := renderPaginationDots(page, pageCount(total, pageSize))
	if dots == "" {
		return ""
	}
	return dots + "\n"
}

func renderConnectionRow(conn connectionItem, now time.Time, layout connectionTableLayout) string {
	columns := []string{
		connectionIDStyle.Render(formatConnectionCell(fmt.Sprintf("#%d", conn.id), layout.idWidth, false)),
		connectionTimeStyle.Render(formatConnectionCell(conn.protocolLabel(), layout.typeWidth, false)),
		connectionSourceStyle.Render(formatConnectionCell(conn.source, layout.fromWidth, false)),
		renderConnectionTargetCell(conn.target, layout.toWidth),
		connectionTimeStyle.Render(formatConnectionCell(formatConnectionAge(now, conn.startedAt), layout.ageWidth, true)),
		trafficUpStyle.Render(formatConnectionCell(human.FormatBytes(conn.uploadBytes()), layout.upWidth, true)),
		trafficDownStyle.Render(formatConnectionCell(human.FormatBytes(conn.downloadBytes()), layout.downWidth, true)),
	}
	return strings.Join(columns, "  ") + "\n"
}

func renderConnectionTargetCell(value string, width int) string {
	value = truncateConnectionTarget(value, width)
	padding := max(0, width-lipgloss.Width(value))
	return renderConnectionTarget(value) + strings.Repeat(" ", padding)
}

func renderConnectionTarget(value string) string {
	before, after, ok := strings.Cut(value, "->")
	if !ok {
		return connectionTargetStyle.Render(value)
	}
	return connectionTargetStyle.Render(before) + connectionArrowStyle.Render("->") + connectionRouteStyle.Render(after)
}

func formatConnectionCell(value string, width int, alignRight bool) string {
	value = truncateConnectionCell(value, width)
	padding := max(0, width-lipgloss.Width(value))
	if alignRight {
		return strings.Repeat(" ", padding) + value
	}
	return value + strings.Repeat(" ", padding)
}

func truncateConnectionCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range value {
		rw := lipgloss.Width(string(r))
		if used+rw+1 > width {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
}

func truncateConnectionTarget(value string, width int) string {
	before, after, ok := strings.Cut(value, "->")
	if !ok || lipgloss.Width(value) <= width || width < 12 {
		return truncateConnectionCell(value, width)
	}

	const arrow = "->"
	prefixWidth := min(18, max(5, (width-lipgloss.Width(arrow))/3))
	suffixWidth := width - prefixWidth - lipgloss.Width(arrow)
	if suffixWidth < 4 {
		return truncateConnectionCell(value, width)
	}
	return truncateConnectionCell(before, prefixWidth) + arrow + truncateConnectionCell(after, suffixWidth)
}
