package tui

import (
	"slices"
	"strings"

	"github.com/rbee3u/efanadp/internal/x365"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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

func newNodeList(items []list.Item) list.Model {
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

func nodeItemsWithCurrent(nodes []x365.Node, currentName string) []list.Item {
	items := make([]list.Item, 0, len(nodes))
	for i := range nodes {
		node := nodes[i]
		items = append(items, nodeItem{node: node, isCurrent: currentName != "" && node.Name == currentName})
	}
	return items
}

func nodesForDisplay(nodes []x365.Node) []x365.Node {
	sorted := append([]x365.Node(nil), nodes...)
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
		if matchesNodeRegion(name, region) {
			return rank
		}
	}
	return len(nodeRegionPreferences)
}

func matchesNodeRegion(name string, region nodeRegionPreference) bool {
	for _, alias := range region.aliases {
		if strings.Contains(name, alias) {
			return true
		}
	}
	return hasRegionCode(name, region.code)
}

func hasRegionCode(name, code string) bool {
	return strings.HasPrefix(name, code) || strings.Contains(name, " "+code) || strings.Contains(name, "-"+code) || strings.Contains(name, "_"+code)
}

func (m *Model) updateNodes(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if m.nodeList.SettingFilter() || (key.String() == "esc" && m.nodeList.FilterState() != list.Unfiltered) {
			var cmd tea.Cmd
			m.nodeList, cmd = m.nodeList.Update(msg)
			return m, cmd
		}

		switch key.String() {
		case "enter":
			selected, ok := m.nodeList.SelectedItem().(nodeItem)
			if !ok {
				m.setStatus(statusWarning, "No runnable nodes")
				return m, nil
			}
			cmd := m.requestStartNode(selected.node, "Stopping current service, then starting node: "+selected.node.Name)
			return m, cmd
		case "esc":
			if m.returnToRunning() {
				return m, nil
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.nodeList, cmd = m.nodeList.Update(msg)
	return m, cmd
}

func (m *Model) updateRunning(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "s":
		m.state = stateNodeList
		m.clearStatus()
		m.setModeHint("Choose a node, press ↵ to confirm switch, esc to cancel")
		m.prepareNodeSelection()
	case "right", "pgdown":
		m.connections.nextPage()
	case "left", "pgup":
		m.connections.previousPage()
	}
	return m, nil
}

func (m *Model) prepareNodeSelection() {
	m.nodeList = newNodeList(nodeItemsWithCurrent(m.nodes, m.currentNodeName()))
	m.resizeNodeList()
	if m.currentNode != nil {
		if _, ok := m.selectNodeByName(m.currentNode.Name); ok {
			return
		}
	}
	m.nodeList.GoToStart()
}

func (m *Model) selectNodeByName(name string) (x365.Node, bool) {
	for i := range m.nodes {
		node := m.nodes[i]
		if node.Name == name {
			m.nodeList.Select(i)
			return node, true
		}
	}
	return x365.Node{}, false
}
