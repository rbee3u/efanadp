package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpItem struct {
	keys  string
	label string
}

func (i helpItem) plain() string {
	if i.keys == "" {
		return i.label
	}
	if i.label == "" {
		return i.keys
	}
	return i.keys + " " + i.label
}

func (i helpItem) render() string {
	if i.keys == "" {
		return helpStyle.Render(i.label)
	}
	if i.label == "" {
		return helpKeyStyle.Render(i.keys)
	}
	return helpKeyStyle.Render(i.keys) + helpStyle.Render(" "+i.label)
}

func renderHelp(items []helpItem, width int) string {
	separatorWidth := lipgloss.Width(" · ")
	var lines []string
	line := make([]helpItem, 0, len(items))
	lineWidth := 0
	for _, item := range items {
		itemWidth := lipgloss.Width(item.plain())
		nextWidth := itemWidth
		if len(line) > 0 {
			nextWidth += separatorWidth
		}
		if width > 0 && len(line) > 0 && lineWidth+nextWidth > width {
			lines = append(lines, renderHelpLine(line))
			line = line[:0]
			lineWidth = 0
		}
		line = append(line, item)
		if lineWidth == 0 {
			lineWidth = itemWidth
		} else {
			lineWidth += separatorWidth + itemWidth
		}
	}
	if len(line) > 0 {
		lines = append(lines, renderHelpLine(line))
	}
	return strings.Join(lines, "\n")
}

func renderHelpLine(items []helpItem) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = item.render()
	}
	return strings.Join(parts, helpStyle.Render(" · "))
}

func (m *Model) helpItems() []helpItem {
	switch m.state {
	case stateNodeList:
		return m.nodeHelp()
	case stateRunning:
		return runningHelp()
	case stateQuitting:
		return []helpItem{{label: "stopping service"}}
	default:
		return runningHelp()
	}
}

func runningHelp() []helpItem {
	return []helpItem{
		{keys: "←/→", label: "page"},
		{keys: "s", label: "switch node"},
		{keys: "q", label: "quit"},
	}
}

func (m *Model) nodeHelp() []helpItem {
	if m.nodeList.SettingFilter() {
		return []helpItem{
			{label: "type to filter"},
			{keys: "↵", label: "apply filter"},
			{keys: "esc", label: "exit search"},
		}
	}

	confirm := "start node"
	if m.service != nil {
		confirm = "confirm switch"
	}
	items := []helpItem{
		{keys: "←/→", label: "page"},
		{keys: "↑/↓", label: "select"},
		{keys: "/", label: "search"},
		{keys: "↵", label: confirm},
	}
	if m.service != nil {
		items = append(items, helpItem{keys: "esc", label: "cancel"})
	}
	return append(items, helpItem{keys: "q", label: "quit"})
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
