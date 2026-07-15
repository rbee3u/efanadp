package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rbee3u/efanadp/internal/x365"
)

func TestNodeItemsFilterOnNodeLabel(t *testing.T) {
	items := nodeItems([]x365.Node{testNode()})
	if len(items) != 1 {
		t.Fatalf("len(items)=%d, want 1", len(items))
	}
	if !strings.Contains(items[0].FilterValue(), "test-node") {
		t.Fatalf("filter value %q does not include node name", items[0].FilterValue())
	}
	item := requireNodeItem(t, items[0])
	if item.Title() != "test-node" {
		t.Fatalf("title=%q, want node name", item.Title())
	}
	if item.Description() != "example.com:443 path=/x365 host=example.net sni=example.net" {
		t.Fatalf("description=%q, want node server detail", item.Description())
	}

	currentItems := nodeItemsWithCurrent([]x365.Node{testNode()}, "test-node")
	current := requireNodeItem(t, currentItems[0])
	if !strings.HasSuffix(current.Title(), " (current)") {
		t.Fatalf("current title=%q, want current marker", current.Title())
	}
}

func TestNodesForDisplaySortsByRegionPriority(t *testing.T) {
	names := []string{"Vietnam01", "United States02", "Japan01", "Singapore01", "Hong Kong02", "United States01", "Taiwan02", "Korea01", "台湾01", "Hong Kong01"}
	nodes := make([]x365.Node, 0, len(names))
	for _, name := range names {
		node := testNode()
		node.Name = name
		nodes = append(nodes, node)
	}

	sorted := nodesForDisplay(nodes)
	want := []string{"Hong Kong01", "Hong Kong02", "Singapore01", "Japan01", "United States01", "United States02", "Taiwan02", "台湾01", "Korea01", "Vietnam01"}
	if len(sorted) != len(want) {
		t.Fatalf("len(sorted)=%d, want %d", len(sorted), len(want))
	}
	for i, name := range want {
		if sorted[i].Name != name {
			t.Fatalf("sorted[%d].Name=%q, want %q", i, sorted[i].Name, name)
		}
	}
	if nodes[0].Name != "Vietnam01" {
		t.Fatalf("nodesForDisplay mutated input order: first=%q", nodes[0].Name)
	}
}

func TestNodeListViewUsesCustomHelpOnly(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.nodeList = newNodeList(nodeItems([]x365.Node{testNode()}))
	model.service = &runningService{}

	view := stripANSI(model.View())
	if strings.Contains(view, "? more") || strings.Contains(view, "/ filter") {
		t.Fatalf("view includes built-in list help:\n%s", view)
	}
	for _, want := range []string{"←/→ page", "↑/↓ select", "/ search", "↵ confirm switch", "esc cancel", "q quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing custom node help %q:\n%s", want, view)
		}
	}
	for _, unexpected := range []string{"g/G", "top/bottom", "k/j", "h/l", "confirm node", "esc back to running"} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("view includes old node help %q:\n%s", unexpected, view)
		}
	}
	if strings.Index(view, "←/→ page") > strings.Index(view, "↵ confirm switch") {
		t.Fatalf("list navigation should appear before the primary action:\n%s", view)
	}
}

func TestNodeListViewShowsStartupHelp(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.nodeList = newNodeList(nodeItems([]x365.Node{testNode()}))

	view := stripANSI(model.View())
	for _, want := range []string{"←/→ page", "↑/↓ select", "/ search", "↵ start node", "q quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing custom node help %q:\n%s", want, view)
		}
	}
	for _, unexpected := range []string{
		"g/G",
		"top/bottom",
		"k/j",
		"h/l",
		"confirm node",
		"esc switch config",
		"switch config",
		"reload config",
	} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("view includes old startup node help %q:\n%s", unexpected, view)
		}
	}
	if strings.Index(view, "←/→ page") > strings.Index(view, "↵ start node") {
		t.Fatalf("list navigation should appear before the primary action:\n%s", view)
	}
	if strings.Contains(view, "? more") || strings.Contains(view, "/ filter") {
		t.Fatalf("view includes built-in list help:\n%s", view)
	}
}

func TestNodeListSwitchHintUsesBottomNoticeRail(t *testing.T) {
	nodes := testNodes(12)
	model := newTestModel(Options{})
	model.state = stateRunning
	model.nodes = nodes
	model.service = &runningService{}
	setRunningNode(model, nodes[3])

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = requireModel(t, updated)
	updated, _ = model.updateRunning(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = requireModel(t, updated)

	view := stripANSI(model.View())
	hint := "Choose a node, press ↵ to confirm switch, esc to cancel"
	titleLine := requireLineContaining(t, view, "Select Node")
	nodeLine := requireLineContaining(t, view, "node-00")
	hintLine := requireLineContaining(t, view, hint)
	helpLine := requireLineContaining(t, view, "←/→ page · ↑/↓ select")

	if hintLine <= titleLine || hintLine <= nodeLine {
		t.Fatalf("node switch hint should stay below list content:\n%s", view)
	}
	if helpLine <= hintLine {
		t.Fatalf("node switch hint should stay above footer help:\n%s", view)
	}
}

func TestRunningStatusUsesBottomNoticeRail(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateRunning
	model.service = &runningService{}
	model.setStatus(statusInfo, "Checking local SOCKS5")

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = requireModel(t, updated)

	view := stripANSI(model.View())
	tableLine := requireLineContaining(t, view, "ID  Type")
	statusLine := requireLineContaining(t, view, "Checking local SOCKS5")
	helpLine := requireLineContaining(t, view, "←/→ page · s switch node · q quit")

	if statusLine <= tableLine {
		t.Fatalf("running status should stay below table content:\n%s", view)
	}
	if helpLine <= statusLine {
		t.Fatalf("running status should stay above footer help:\n%s", view)
	}
}

func TestInfoNoticesUseMutedTextAndAccentKeys(t *testing.T) {
	if got := statusInfoStyle.GetForeground(); got != colorMuted {
		t.Fatalf("info status foreground=%v, want muted guidance color %v", got, colorMuted)
	}
	guidance := renderGuidance("press enter to confirm or esc to cancel")
	for _, key := range []string{"enter", "esc"} {
		if !strings.Contains(guidance, helpKeyStyle.Render(key)) {
			t.Fatalf("guidance does not accent key %q: %q", key, guidance)
		}
	}
}

func TestNodeTitleStyleHighlightsTwoLineItems(t *testing.T) {
	if got := nodeTitleStyle.GetForeground(); got != colorAccent {
		t.Fatalf("node title foreground=%v, want accent color %v", got, colorAccent)
	}
	if got := nodeDetailStyle.GetForeground(); got != colorMuted {
		t.Fatalf("node detail foreground=%v, want muted color %v", got, colorMuted)
	}
	if delegate := styledNodeDelegate(); !delegate.ShowDescription {
		t.Fatal("node delegate should render node details as a second line")
	}
}

func TestNodeListViewShowsPaginationDots(t *testing.T) {
	nodes := testNodes(40)
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.nodes = nodes
	model.nodeList = newNodeList(nodeItems(nodes))

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = requireModel(t, updated)
	assertNodePaginationDots(t, model)
}

func TestNodeListPaginationReservesHeightAtPageBoundary(t *testing.T) {
	var model *Model
	var nodes []x365.Node
	found := false
	for height := 14; height <= 40; height++ {
		base := newTestModel(Options{})
		updated, _ := base.Update(tea.WindowSizeMsg{Width: 100, Height: height})
		candidate := requireModel(t, updated)

		noReserve := newNodeList(nil)
		noReserve.Paginator.TotalPages = 1
		noReserve.SetSize(candidate.mainContentWidth(), candidate.nodeListHeight())

		withReserve := newNodeList(nil)
		withReserve.Paginator.TotalPages = 2
		withReserve.SetSize(candidate.mainContentWidth(), candidate.nodeListHeight())

		if withReserve.Paginator.PerPage < noReserve.Paginator.PerPage {
			count := withReserve.Paginator.PerPage + 1
			if count <= noReserve.Paginator.PerPage {
				model = candidate
				nodes = testNodes(count)
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("could not find a pagination boundary fixture")
	}

	model.state = stateNodeList
	model.nodes = nodes
	model.nodeList = newNodeList(nodeItems(nodes))
	model.resizeNodeList()

	assertNodePaginationDots(t, model)
}

func TestNodeListPaginationVisibleAfterSelectingCurrentNodePage(t *testing.T) {
	nodes := testNodes(80)
	current := nodes[60]
	model := newTestModel(Options{})
	model.state = stateRunning
	model.nodes = nodes
	model.service = &runningService{}
	setRunningNode(model, current)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 128, Height: 42})
	model = requireModel(t, updated)
	updated, _ = model.updateRunning(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = requireModel(t, updated)

	if model.state != stateNodeList {
		t.Fatalf("state=%v, want node list", model.state)
	}
	if model.nodeList.Paginator.Page == 0 {
		t.Fatalf("node list page=%d, want current node page", model.nodeList.Paginator.Page)
	}
	assertNodePaginationDots(t, model)
}

// assertNodePaginationDots checks the node list footer renders exactly one dot
// per page: no fewer (dots clipped by the height budget) and no more (a dot
// style that accidentally doubles each bullet). Bullets (U+2022) only appear in
// the paginator here, so counting them across the whole view is unambiguous.
func assertNodePaginationDots(t *testing.T, model *Model) {
	t.Helper()
	pages := model.nodeList.Paginator.TotalPages
	if pages < 2 {
		t.Fatalf("node list pages=%d, want multiple pages", pages)
	}
	view := stripANSI(model.View())
	if got := strings.Count(view, "•"); got != pages {
		t.Fatalf("pagination dots=%d, want exactly one per page (%d); perPage=%d page=%d nodeListHeight=%d\n%s",
			got,
			pages,
			model.nodeList.Paginator.PerPage,
			model.nodeList.Paginator.Page,
			model.nodeListHeight(),
			view)
	}
}

func requireLineContaining(t *testing.T, view, want string) int {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("view missing line containing %q:\n%s", want, view)
	return -1
}

func TestWindowFrameShowsBrandAndIdleSummary(t *testing.T) {
	model := newTestModel(Options{})

	view := stripANSI(model.View())
	for _, want := range []string{"╭", "╰", "┌", "└", "efanadp", "Not running", "Select Node"} {
		if !strings.Contains(view, want) {
			t.Fatalf("idle window missing %q:\n%s", want, view)
		}
	}
	for _, unexpected := range []string{"Config file: ", "Node: ", "Local SOCKS5: "} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("idle window includes running metadata %q:\n%s", unexpected, view)
		}
	}
}

func TestWindowHeaderBannerAdaptsToTerminalSize(t *testing.T) {
	model := newTestModel(Options{})

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	spacious := requireModel(t, updated)
	view := stripANSI(spacious.View())
	if !strings.Contains(view, "█") {
		t.Fatalf("spacious window missing ASCII banner:\n%s", view)
	}
	firstBannerLine := -1
	notRunningLine := -1
	bannerLine := ""
	for i, line := range strings.Split(view, "\n") {
		if firstBannerLine == -1 && strings.Contains(line, "█") {
			firstBannerLine = i
		}
		if strings.Contains(line, "Not running") {
			notRunningLine = i
		}
		if strings.Contains(line, "█") && strings.Contains(line, "Not running") {
			bannerLine = line
		}
	}
	if bannerLine == "" {
		t.Fatalf("summary should sit beside the banner, not below it:\n%s", view)
	}
	if notRunningLine != firstBannerLine+1 {
		t.Fatalf("idle summary should sit one line below the banner top:\n%s", view)
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 50, Height: 16})
	small := requireModel(t, updated)
	view = stripANSI(small.View())
	if strings.Contains(view, "█") {
		t.Fatalf("small window should fall back to plain brand text:\n%s", view)
	}
	if !strings.Contains(view, "efanadp") {
		t.Fatalf("small window missing plain brand text:\n%s", view)
	}
}

func TestViewFillsTerminalHeightRegardlessOfConnectionCount(t *testing.T) {
	model := newTestModel(Options{ConfigPath: "config.yaml"})
	model.service = &runningService{listen: "127.0.0.1:10865"}
	setRunningNode(model, testNode())
	model.state = stateRunning

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	model = requireModel(t, updated)

	model.connections.active = map[uint64]connectionItem{}
	emptyHeight := lipgloss.Height(model.View())

	model.connections.active = map[uint64]connectionItem{}
	for i := range 40 {
		id := uint64(i + 1)
		model.connections.active[id] = connectionItem{id: id, source: "client", target: "example.com:443"}
	}
	fullHeight := lipgloss.Height(model.View())

	if emptyHeight != 32 || fullHeight != 32 {
		t.Fatalf("view heights empty=%d full=%d, want both to fill terminal height 32", emptyHeight, fullHeight)
	}
}

func TestViewNeverExceedsTerminalHeightKeepingTopBorder(t *testing.T) {
	sizes := []tea.WindowSizeMsg{{Width: 140, Height: 20}, {Width: 100, Height: 22}, {Width: 80, Height: 16}}
	for _, size := range sizes {
		model := newTestModel(Options{ConfigPath: "config.yaml"})
		model.service = &runningService{listen: "127.0.0.1:10865"}
		setRunningNode(model, testNode())
		model.nodes = []x365.Node{testNode()}
		model.state = stateNodeList
		model.setModeHint("Choose a node, press ↵ to confirm switch, esc to cancel")
		model.nodeList = newNodeList(nodeItemsWithCurrent(model.nodes, model.currentNodeName()))
		updated, _ := model.Update(size)
		model = requireModel(t, updated)

		view := stripANSI(model.View())
		if got := lipgloss.Height(view); got > size.Height {
			t.Fatalf("node view height=%d exceeds terminal height=%d at %dx%d:\n%s", got, size.Height, size.Width, size.Height, view)
		}
		if !strings.HasPrefix(view, "╭") {
			t.Fatalf("node view lost its top border at %dx%d:\n%s", size.Width, size.Height, view)
		}
	}
}

func TestWindowFrameFitsTerminalWidth(t *testing.T) {
	model := newTestModel(Options{})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	model = requireModel(t, updated)

	view := stripANSI(model.View())
	for line := range strings.SplitSeq(view, "\n") {
		if width := lipgloss.Width(line); width > model.windowWidth() {
			t.Fatalf("window line width=%d exceeds terminal width=%d:\n%s", width, model.windowWidth(), view)
		}
	}
	if !strings.Contains(view, "╮") || !strings.Contains(view, "╯") {
		t.Fatalf("window is missing right border corners:\n%s", view)
	}
}

func TestRunningSummaryShowsOnNodeAndRunningViews(t *testing.T) {
	model := newTestModel(Options{ConfigPath: "config.yaml"})
	model.service = &runningService{}
	setRunningNode(model, testNode())
	model.connections.active = map[uint64]connectionItem{
		1: {id: 1},
		2: {id: 2},
	}
	model.connections.closed = 3

	model.state = stateNodeList
	model.nodes = []x365.Node{testNode()}
	model.nodeList = newNodeList(nodeItemsWithCurrent(model.nodes, model.currentNodeName()))
	nodeView := stripANSI(model.View())
	assertWindowRunningSummary(t, nodeView)
	for _, want := range []string{
		"Running · Active: 2 · Closed: 3 · URL: socks5://127.0.0.1:10865#efanadp",
		"test-node",
		"example.com:443 path=/x365 host=example.net sni=example.net",
	} {
		if !strings.Contains(nodeView, want) {
			t.Fatalf("node view missing running summary %q:\n%s", want, nodeView)
		}
	}
	for _, unexpected := range []string{
		"Config file:",
		"Node:",
		"Local SOCKS5:",
		"Listen address:",
		"Log path:",
		"Config path:",
		"Node name:",
		"Node parameters:",
	} {
		if strings.Contains(nodeView, unexpected) {
			t.Fatalf("node view includes old running summary field %q:\n%s", unexpected, nodeView)
		}
	}
	if strings.Contains(nodeView, "Selected:") || strings.Contains(nodeView, "server:") {
		t.Fatalf("node view should not include selected-node detail block:\n%s", nodeView)
	}
	summaryLines := strings.Split(stripANSI(model.renderRunningSummary(120)), "\n")
	if len(summaryLines) < 4 {
		t.Fatalf("running summary should include status, gap, node, and detail lines:\n%s", strings.Join(summaryLines, "\n"))
	}
	if strings.TrimSpace(summaryLines[1]) != "" || strings.TrimSpace(summaryLines[2]) != "test-node" {
		t.Fatalf("running summary should leave a gap before node title:\n%s", strings.Join(summaryLines, "\n"))
	}

	model.state = stateRunning
	runningView := stripANSI(model.View())
	assertWindowRunningSummary(t, runningView)
	if strings.Count(runningView, "Running · Active: 2 · Closed: 3 · URL: socks5://127.0.0.1:10865#efanadp") != 1 {
		t.Fatalf("running summary should be rendered once in the window header:\n%s", runningView)
	}
}

func TestLocalSOCKS5LinkUsesLoopbackForWildcardListen(t *testing.T) {
	got := localSOCKS5Link("0.0.0.0:10865")
	want := "socks5://127.0.0.1:10865#efanadp"
	if got != want {
		t.Fatalf("link=%q, want %q", got, want)
	}
}

func TestNarrowRunningSummaryUsesCompactMetadata(t *testing.T) {
	model := newTestModel(Options{ConfigPath: "config.yaml"})
	model.service = &runningService{listen: "127.0.0.1:10865"}
	setRunningNode(model, testNode())
	model.connections.active = map[uint64]connectionItem{1: {id: 1}}

	summary := stripANSI(model.renderRunningSummary(50))
	for _, want := range []string{
		"socks5://127.0.0.1:10865#efanadp",
		"Active: 1",
		"Closed: 0",
		"test-node",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("compact summary missing %q:\n%s", want, summary)
		}
	}
	for _, unexpected := range []string{"Config file:", "Node:", "Local SOCKS5:", "path=/x365", "host=example.net", "sni=example.net"} {
		if strings.Contains(summary, unexpected) {
			t.Fatalf("compact summary includes verbose metadata %q:\n%s", unexpected, summary)
		}
	}
}

func TestNodeListEscClearsFilterBeforeReturning(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.nodeList = newNodeList(nodeItems([]x365.Node{testNode()}))
	model.service = &runningService{}

	updated, _ := model.updateNodes(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = requireModel(t, updated)
	if !model.nodeList.SettingFilter() {
		t.Fatal("expected slash to enter filter mode")
	}

	updated, _ = model.updateNodes(tea.KeyMsg{Type: tea.KeyEsc})
	model = requireModel(t, updated)
	if model.state != stateNodeList {
		t.Fatalf("state=%v, want nodes after clearing filter", model.state)
	}
	if model.nodeList.SettingFilter() || model.nodeList.IsFiltered() {
		t.Fatal("expected esc to clear filter before returning to running view")
	}

	updated, _ = model.updateNodes(tea.KeyMsg{Type: tea.KeyEsc})
	model = requireModel(t, updated)
	if model.state != stateRunning {
		t.Fatalf("state=%v, want running after second esc", model.state)
	}
}

func TestNodeListFilterConsumesShortcutKeys(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.nodeList = newNodeList(nodeItems([]x365.Node{testNode()}))
	model.service = &runningService{}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = requireModel(t, updated)
	if !model.nodeList.SettingFilter() {
		t.Fatal("expected slash to enter filter mode")
	}
	if view := stripANSI(model.View()); !strings.Contains(view, "type to filter · ↵ apply filter · esc exit search") {
		t.Fatalf("filter view missing global-key hint:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = requireModel(t, updated)
	if model.state != stateNodeList {
		t.Fatalf("state=%v, want nodes while typing in filter", model.state)
	}
	if got := model.nodeList.FilterValue(); got != "q" {
		t.Fatalf("filter value=%q, want q", got)
	}
}

func TestNodeListUsesMainPaneDimensions(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateNodeList
	model.nodeList = newNodeList(nodeItems([]x365.Node{testNode()}))

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	model = requireModel(t, updated)
	if got, want := model.nodeList.Width(), model.mainContentWidth(); got != want {
		t.Fatalf("node list width=%d, want main content width %d", got, want)
	}
	if got, want := model.nodeList.Height(), model.nodeListHeight(); got != want {
		t.Fatalf("node list height=%d, want main content height %d", got, want)
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 50, Height: 16})
	model = requireModel(t, updated)
	if got, want := model.nodeList.Width(), model.mainContentWidth(); got != want {
		t.Fatalf("small node list width=%d, want main content width %d", got, want)
	}
	if got := model.nodeList.Height(); got < 8 {
		t.Fatalf("small node list height=%d, want minimum usable height", got)
	}
}

func TestRunningSwitchResetsNodeListToCurrentNode(t *testing.T) {
	current := testNode()
	other := testNode()
	other.Name = "other-node"
	other.Server = "other.example.com"

	model := newTestModel(Options{})
	model.state = stateRunning
	model.nodes = []x365.Node{other, current}
	model.nodeList = newNodeList(nodeItems(model.nodes))
	model.service = &runningService{}
	setRunningNode(model, current)
	model.nodeList.SetFilterText("other")
	model.nodeList.Select(0)

	updated, _ := model.updateRunning(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = requireModel(t, updated)
	if model.state != stateNodeList {
		t.Fatalf("state=%v, want nodes", model.state)
	}
	if model.nodeList.IsFiltered() || model.nodeList.SettingFilter() || model.nodeList.FilterValue() != "" {
		t.Fatalf("expected node filter to reset, state=%v value=%q", model.nodeList.FilterState(), model.nodeList.FilterValue())
	}
	selected, ok := model.nodeList.SelectedItem().(nodeItem)
	if !ok {
		t.Fatal("expected selected node item")
	}
	if selected.node.Name != current.Name {
		t.Fatalf("selected node=%q, want current node %q", selected.node.Name, current.Name)
	}
}

func testNodes(count int) []x365.Node {
	nodes := make([]x365.Node, 0, count)
	for i := range count {
		node := testNode()
		node.Name = fmt.Sprintf("node-%02d", i)
		nodes = append(nodes, node)
	}
	return nodes
}
