package tui

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/x365"
)

type nodeItem struct {
	node      x365.Node
	isCurrent bool
}

type nodeRegionPreference struct {
	aliases []string
	code    string
}

var nodeRegionPreferences = []nodeRegionPreference{
	{aliases: []string{"hong kong", "香港"}, code: "hk"},
	{aliases: []string{"singapore", "新加坡"}, code: "sg"},
	{aliases: []string{"japan", "日本"}, code: "jp"},
	{aliases: []string{"united states", "usa", "美国"}, code: "us"},
	{aliases: []string{"taiwan", "台湾"}, code: "tw"},
}

func (i nodeItem) Title() string {
	title := i.node.Name
	if i.isCurrent {
		title += " (current)"
	}
	return title
}

func (i nodeItem) Description() string {
	return i.node.ServerSummary()
}

func (i nodeItem) FilterValue() string {
	return i.node.Label()
}

func newNodeList(nodes []x365.Node, currentName string) list.Model {
	items := make([]list.Item, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, nodeItem{node: node, isCurrent: currentName != "" && node.Name == currentName})
	}
	l := list.New(items, styledNodeDelegate(), 80, 18)
	l.Title = "Select Node"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(true)
	l.SetFilteringEnabled(true)
	l.SetStatusBarItemName("node", "nodes")
	l.FilterInput.Prompt = "Filter: "
	applyNodeListStyle(&l)
	return l
}

func nodesForDisplay(nodes []x365.Node) []x365.Node {
	sorted := slices.Clone(nodes)
	slices.SortFunc(sorted, func(left, right x365.Node) int {
		leftRank := nodeRegionPreferenceRank(left.Name)
		rightRank := nodeRegionPreferenceRank(right.Name)
		if leftRank != rightRank {
			return leftRank - rightRank
		}
		return strings.Compare(left.Name, right.Name)
	})
	return sorted
}

func nodeRegionPreferenceRank(name string) int {
	name = strings.ToLower(name)
	for rank, region := range nodeRegionPreferences {
		for _, alias := range region.aliases {
			if strings.Contains(name, alias) {
				return rank
			}
		}
		if hasRegionCode(name, region.code) {
			return rank
		}
	}
	return len(nodeRegionPreferences)
}

func hasRegionCode(name, code string) bool {
	return strings.HasPrefix(name, code) || strings.Contains(name, " "+code) || strings.Contains(name, "-"+code) || strings.Contains(name, "_"+code)
}

func (m *Model) updateNodes(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyMsg); ok && !m.nodeList.SettingFilter() {
		switch key.String() {
		case "enter":
			selected, ok := m.nodeList.SelectedItem().(nodeItem)
			if !ok {
				m.setStatus(statusWarning, "No runnable nodes")
				return nil
			}
			return m.requestStartNode(selected.node)
		case "esc":
			if m.nodeList.FilterState() == list.Unfiltered {
				m.returnToRunning()
				return nil
			}
		}
	}

	var cmd tea.Cmd
	m.nodeList, cmd = m.nodeList.Update(msg)
	return cmd
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

func (m *Model) prepareNodeSelection() {
	currentName := m.currentNodeName()
	m.nodeList = newNodeList(m.nodes, currentName)
	m.resizeNodeList()
	if currentName != "" {
		if _, ok := m.selectNodeByName(currentName); ok {
			return
		}
	}
	m.nodeList.GoToStart()
}

func (m *Model) selectNodeByName(name string) (x365.Node, bool) {
	for i, node := range m.nodes {
		if node.Name == name {
			m.nodeList.Select(i)
			return node, true
		}
	}
	return x365.Node{}, false
}

func (m *Model) returnToRunning() {
	if m.service == nil {
		return
	}
	m.state = stateRunning
	m.clearModeHint()
	m.setExpiringStatus(statusInfo, "Cancelled; current service keeps running", 2*time.Second)
}
