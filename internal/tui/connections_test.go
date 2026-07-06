package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/proxy"
)

func BenchmarkActiveConnectionsView(b *testing.B) {
	model := newTestModel(Options{})
	model.state = stateRunning
	model.service = &runningService{id: 1, nodeName: "bench"}
	model.width = 160
	model.height = 48
	model.connections.active = make(map[uint64]connectionItem)
	base := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	for i := range 1000 {
		id := uint64(i + 1)
		model.connections.apply(proxy.Event{
			ID:        id,
			Type:      proxy.ConnectionOpened,
			Source:    fmt.Sprintf("127.0.0.1:%d", 5000+i),
			Target:    fmt.Sprintf("example-%04d.com:443", i),
			StartedAt: base.Add(time.Duration(i) * time.Millisecond),
			Stats:     &proxy.Stats{},
		})
	}
	model.connections.now = base.Add(time.Minute)

	b.ReportAllocs()
	for b.Loop() {
		if view := model.View(); view == "" {
			b.Fatal("empty view")
		}
	}
}

func TestConnectionEventsUpdateActiveAndClosedCounts(t *testing.T) {
	model := newTestModel(Options{})
	model.service = &runningService{id: 7, connectionEvents: make(chan proxy.Event)}
	startedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	updated, _ := model.Update(connectionEventMsg{
		serviceID: 7,
		event: proxy.Event{
			ID:        1,
			Type:      proxy.ConnectionOpened,
			Protocol:  "udp",
			Source:    "127.0.0.1:50000",
			Target:    "example.com:443",
			StartedAt: startedAt,
		},
	})
	model = requireModel(t, updated)
	if got := model.connections.activeCount(); got != 1 {
		t.Fatalf("active connections=%d, want 1", got)
	}
	if got := model.connections.active[1].protocol; got != "udp" {
		t.Fatalf("connection protocol=%q, want udp", got)
	}
	if model.connections.closed != 0 {
		t.Fatalf("closed connections=%d, want 0", model.connections.closed)
	}
	if model.status != "" {
		t.Fatalf("status=%q, want connection events to keep transient status unchanged", model.status)
	}

	updated, _ = model.Update(connectionEventMsg{
		serviceID: 7,
		event: proxy.Event{
			ID:        1,
			Type:      proxy.ConnectionUpdated,
			Protocol:  "udp",
			Source:    "127.0.0.1:50000",
			Target:    "example.com:443->inner.example:443",
			StartedAt: startedAt,
		},
	})
	model = requireModel(t, updated)
	if got := model.connections.active[1].target; got != "example.com:443->inner.example:443" {
		t.Fatalf("connection target=%q, want updated chain", got)
	}
	if got := model.connections.activeCount(); got != 1 {
		t.Fatalf("active connections=%d, want 1 after update", got)
	}

	updated, _ = model.Update(connectionEventMsg{
		serviceID: 7,
		event: proxy.Event{
			ID:        1,
			Type:      proxy.ConnectionClosed,
			Source:    "127.0.0.1:50000",
			Target:    "example.com:443",
			StartedAt: startedAt,
			ClosedAt:  startedAt.Add(time.Second),
		},
	})
	model = requireModel(t, updated)
	if got := model.connections.activeCount(); got != 0 {
		t.Fatalf("active connections=%d, want 0", got)
	}
	if model.connections.closed != 1 {
		t.Fatalf("closed connections=%d, want 1", model.connections.closed)
	}
	if model.status != "" {
		t.Fatalf("status=%q, want connection events to keep transient status unchanged", model.status)
	}
}

func TestRunningViewShowsPagedActiveConnections(t *testing.T) {
	startedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	model := newTestModel(Options{ConfigPath: "proxy.yaml"})
	model.state = stateRunning
	model.service = &runningService{}
	model.setCurrentNode(testNode())
	model.connections.active = make(map[uint64]connectionItem)
	model.connections.closed = 2
	model.connections.now = startedAt.Add(90 * time.Second)
	for i := range 55 {
		id := uint64(i + 1)
		model.connections.active[id] = connectionItem{
			id:        id,
			source:    fmt.Sprintf("client-%02d", i),
			target:    fmt.Sprintf("target-%02d:443", i),
			startedAt: startedAt.Add(time.Duration(i) * time.Second),
		}
	}

	view := stripANSI(model.View())
	if !strings.Contains(view, "Running · Active: 55 · Closed: 2 · URL: socks5://127.0.0.1:10865#efanadp") {
		t.Fatalf("view missing English connection status:\n%s", view)
	}
	if strings.Contains(view, "upload=") || strings.Contains(view, "download=") {
		t.Fatalf("view contains old transfer field names:\n%s", view)
	}
	if !strings.Contains(view, "host=example.net") || !strings.Contains(view, "sni=example.net") {
		t.Fatalf("view does not include node host and sni:\n%s", view)
	}
	if strings.Contains(view, "Active connections 1-30/55") {
		t.Fatalf("view still shows connection page as a top title:\n%s", view)
	}
	if strings.Contains(view, "1-30/55") || strings.Contains(view, "page 1/2") {
		t.Fatalf("view still shows the old text page summary:\n%s", view)
	}
	if got := strings.Count(view, "•"); got != 2 {
		t.Fatalf("first connection page dots=%d, want 2 (one per page):\n%s", got, view)
	}
	if !strings.Contains(view, "ID") || !strings.Contains(view, "Type") || !strings.Contains(view, "Source") || !strings.Contains(view, "Target") || !strings.Contains(view, "Age") || !strings.Contains(view, "Up") || !strings.Contains(view, "Down") {
		t.Fatalf("view does not include connection table header:\n%s", view)
	}
	if strings.Contains(view, "source=") || strings.Contains(view, "target=") || strings.Contains(view, "time=") || strings.Contains(view, "up=") || strings.Contains(view, "down=") {
		t.Fatalf("view contains field labels in connection data:\n%s", view)
	}
	if !strings.Contains(view, "client-54") || !strings.Contains(view, "client-25") {
		t.Fatalf("view does not include first page connections:\n%s", view)
	}
	if strings.Contains(view, "client-24") {
		t.Fatalf("view includes second page connection on first page:\n%s", view)
	}

	updated, _ := model.updateRunning(tea.KeyMsg{Type: tea.KeyPgDown})
	model = requireModel(t, updated)
	view = stripANSI(model.View())
	if strings.Contains(view, "31-55/55") || strings.Contains(view, "page 2/2") {
		t.Fatalf("view still shows the old text page summary:\n%s", view)
	}
	if got := strings.Count(view, "•"); got != 2 {
		t.Fatalf("second connection page dots=%d, want 2 (one per page):\n%s", got, view)
	}
	if !strings.Contains(view, "client-24") || !strings.Contains(view, "client-00") {
		t.Fatalf("view does not include second page connections:\n%s", view)
	}
	if strings.Contains(view, "client-25") {
		t.Fatalf("view includes first page connection on second page:\n%s", view)
	}
}

func TestConnectionPageClampsAfterResize(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateRunning
	model.service = &runningService{}
	model.connections.active = make(map[uint64]connectionItem)
	for i := range 3 {
		id := uint64(i + 1)
		model.connections.active[id] = connectionItem{id: id}
	}
	model.connections.pageSize = 1
	model.connections.page = 2

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 48})
	model = requireModel(t, updated)
	if model.connections.page != 0 {
		t.Fatalf("connection page=%d, want clamped first page after resize", model.connections.page)
	}
	rows, total := model.connections.pageItems(model.connections.page)
	if total != 3 || len(rows) == 0 {
		t.Fatalf("page rows=%d total=%d, want non-empty clamped page", len(rows), total)
	}
	if view := stripANSI(model.runningBody()); strings.Contains(view, "No active connections") {
		t.Fatalf("running body shows false empty state after resize:\n%s", view)
	}
}

func TestRunningViewAlwaysShowsPagingHelp(t *testing.T) {
	model := newTestModel(Options{ConfigPath: "proxy.yaml"})
	model.state = stateRunning
	model.connections.active = make(map[uint64]connectionItem)

	view := stripANSI(model.View())
	if !strings.Contains(view, "←/→ page · s switch node · q quit") {
		t.Fatalf("running view missing fixed help:\n%s", view)
	}
}

func TestConnectionRowsUseCompactEmptyState(t *testing.T) {
	var empty strings.Builder
	writeConnectionRows(&empty, nil, time.Time{}, 100, 0, connectionPageSize, 0)
	emptyView := stripANSI(empty.String())
	if got := strings.Count(emptyView, "\n"); got >= connectionPageSize {
		t.Fatalf("empty connection rows=%d, want compact empty state", got)
	}
	if !strings.Contains(emptyView, "No active connections") || !strings.Contains(emptyView, "local SOCKS5") {
		t.Fatalf("empty connection rows missing placeholder:\n%s", emptyView)
	}
	for _, unexpected := range []string{"0/0", "page 0/0", "page 1/1"} {
		if strings.Contains(emptyView, unexpected) {
			t.Fatalf("empty connection rows should hide page summary %q:\n%s", unexpected, emptyView)
		}
	}

	stats := &proxy.Stats{}
	stats.AddUpload(1536)
	stats.AddDownload(42)
	var one strings.Builder
	writeConnectionRows(&one, []connectionItem{{
		id:        1,
		protocol:  "udp",
		source:    "client",
		target:    "example.com:443",
		startedAt: time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
		stats:     stats,
	}}, time.Date(2026, 6, 30, 12, 0, 1, 0, time.UTC), 100, 0, connectionPageSize, 1)
	oneView := stripANSI(one.String())
	if got := strings.Count(oneView, "\n"); got >= 10 {
		t.Fatalf("one connection rows=%d, want compact table:\n%s", got, oneView)
	}
	if !strings.Contains(oneView, "client") || !strings.Contains(oneView, "example.com:443") {
		t.Fatalf("one connection rows missing connection:\n%s", oneView)
	}
	if !strings.Contains(oneView, "udp") {
		t.Fatalf("one connection rows missing protocol type:\n%s", oneView)
	}
	if strings.Contains(oneView, "source=client") || strings.Contains(oneView, "time=1s") || strings.Contains(oneView, "up=1.5KiB") || strings.Contains(oneView, "down=42B") {
		t.Fatalf("one connection rows still include field labels:\n%s", oneView)
	}
	if !strings.Contains(oneView, "1s") || !strings.Contains(oneView, "1.5KiB") || !strings.Contains(oneView, "42B") {
		t.Fatalf("one connection rows missing unified fields:\n%s", oneView)
	}
	if strings.Contains(oneView, "•") || strings.Contains(oneView, "1-1/1") || strings.Contains(oneView, "page 1/1") {
		t.Fatalf("single connection page should hide pagination:\n%s", oneView)
	}
}

func TestConnectionPaginationDotsStayPinnedAcrossPages(t *testing.T) {
	const pageSize = 10
	const total = 25 // three pages: 10, 10, 5

	dotLine := func(page int) int {
		var b strings.Builder
		var rows []connectionItem
		start, end, _ := pageRange(page, total, pageSize)
		for i := start; i < end; i++ {
			if i < 0 {
				t.Fatalf("pageRange returned negative index: %d", i)
				return 0
			}
			id := uint64(i) + 1
			rows = append(rows, connectionItem{id: id})
		}
		writeConnectionRows(&b, rows, time.Time{}, 100, page, pageSize, total)
		lines := strings.Split(stripANSI(b.String()), "\n")
		for i, line := range lines {
			if strings.Contains(line, "•") {
				return i
			}
		}
		t.Fatalf("page %d has no pagination dots:\n%s", page, b.String())
		return -1
	}

	full := dotLine(0)    // a full page
	partial := dotLine(2) // the short last page
	if full != partial {
		t.Fatalf("pagination dots move between pages: full-page line=%d, last-page line=%d", full, partial)
	}
}

func TestConnectionPaginationRendersOneDotPerPage(t *testing.T) {
	cases := []struct {
		total, pageSize, page, wantDots int
	}{
		{total: 0, pageSize: 10, page: 0, wantDots: 0},  // empty state
		{total: 5, pageSize: 10, page: 0, wantDots: 0},  // single page hides dots
		{total: 25, pageSize: 10, page: 0, wantDots: 3}, // first of three pages
		{total: 25, pageSize: 10, page: 2, wantDots: 3}, // last of three pages
	}
	for _, tc := range cases {
		var b strings.Builder
		writeConnectionRows(&b, nil, time.Time{}, 100, tc.page, tc.pageSize, tc.total)
		view := stripANSI(b.String())
		if got := strings.Count(view, "•"); got != tc.wantDots {
			t.Fatalf("total=%d pageSize=%d page=%d: dots=%d, want exactly one per page (%d)\n%s",
				tc.total, tc.pageSize, tc.page, got, tc.wantDots, view)
		}
	}
}

func TestConnectionRowsTruncateLongTargets(t *testing.T) {
	var b strings.Builder
	writeConnectionRows(&b, []connectionItem{{
		id:        28,
		source:    "127.0.0.1:59322",
		target:    "203.0.113.10:39281->very-long-service-name.with.many.labels.example.com:443",
		startedAt: time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	}}, time.Date(2026, 6, 30, 12, 0, 31, 0, time.UTC), 78, 0, connectionPageSize, 1)

	view := stripANSI(b.String())
	if !strings.Contains(view, "Target") {
		t.Fatalf("connection table missing target header:\n%s", view)
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("connection table does not truncate long target:\n%s", view)
	}
	if strings.Contains(view, "very-long-service-name.with.many.labels.example.com") {
		t.Fatalf("connection table includes untruncated long target:\n%s", view)
	}
}

func TestConnectionTargetArrowUsesSeparateStyle(t *testing.T) {
	rendered := renderConnectionTarget("203.0.113.10:39281->my.1password.com:443")
	plain := stripANSI(rendered)
	if plain != "203.0.113.10:39281->my.1password.com:443" {
		t.Fatalf("plain target=%q", plain)
	}
	if connectionArrowStyle.GetForeground() != colorSubtle {
		t.Fatalf("arrow foreground=%v, want subtle color", connectionArrowStyle.GetForeground())
	}
	if connectionArrowStyle.GetForeground() == connectionRouteStyle.GetForeground() {
		t.Fatal("arrow should not use the same foreground as the destination route")
	}
}

func TestConnectionTargetColumnCapsAtEighty(t *testing.T) {
	target := "203.0.113.10:39281->" + strings.Repeat("target.", 20) + "example.com:443"
	row := connectionItem{
		id:        28,
		source:    "127.0.0.1:59322",
		target:    target,
		startedAt: time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC),
	}
	layout := newConnectionTableLayout([]connectionItem{row}, time.Date(2026, 6, 30, 12, 0, 31, 0, time.UTC), 220)
	if layout.toWidth != 80 {
		t.Fatalf("target column width=%d, want 80", layout.toWidth)
	}

	rendered := stripANSI(renderConnectionRow(row, time.Date(2026, 6, 30, 12, 0, 31, 0, time.UTC), layout))
	if !strings.Contains(rendered, "…") {
		t.Fatalf("connection row does not truncate wide target:\n%s", rendered)
	}
	if strings.Contains(rendered, target) {
		t.Fatalf("connection row includes untruncated target:\n%s", rendered)
	}
}

func TestRunningConnectionPagesUseArrowKeys(t *testing.T) {
	model := newTestModel(Options{})
	model.state = stateRunning
	model.status = model.runningStatus()
	model.connections.active = make(map[uint64]connectionItem)
	for i := range connectionPageSize + 1 {
		id := uint64(i + 1)
		model.connections.active[id] = connectionItem{id: id}
	}
	model.status = model.runningStatus()

	updated, _ := model.updateRunning(tea.KeyMsg{Type: tea.KeyRight})
	model = requireModel(t, updated)
	if model.connections.page != 1 {
		t.Fatalf("connection page=%d, want next page", model.connections.page)
	}
	if strings.Contains(model.status, "page 2/2") || !strings.Contains(model.status, "Running") {
		t.Fatalf("status=%q, want running status without page text", model.status)
	}

	updated, _ = model.updateRunning(tea.KeyMsg{Type: tea.KeyLeft})
	model = requireModel(t, updated)
	if model.connections.page != 0 {
		t.Fatalf("connection page=%d, want previous page", model.connections.page)
	}
	if strings.Contains(model.status, "page 1/2") || !strings.Contains(model.status, "Running") {
		t.Fatalf("status=%q, want running status without page text", model.status)
	}
}
