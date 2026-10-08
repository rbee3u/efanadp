package tui

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rbee3u/efanadp/internal/proxy"
	"github.com/rbee3u/efanadp/internal/session"
	"github.com/rbee3u/efanadp/internal/x365"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[\x20-\x2f]*[\x40-\x7e]`)

const testRealityPublicKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func newTestModel(opts Options) *Model {
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:10865"
	}
	if opts.Prepare == nil {
		opts.Prepare = func(x365.Node) (session.StartFunc, error) {
			return func() session.Service { return newFakeService() }, nil
		}
	}
	return newModel(opts)
}

func setRunningNode(model *Model, node x365.Node) {
	if model.service == nil {
		model.service = &runningService{}
	}
	model.service.node = node
}

func assertWindowRunningSummary(t *testing.T, view string) {
	t.Helper()
	if !strings.Contains(view, "efanadp") {
		t.Fatalf("view missing app brand:\n%s", view)
	}
	status := "Running · Active: 2 · Closed: 3 · URL: socks5://127.0.0.1:10865#efanadp"
	if !strings.Contains(view, status) {
		t.Fatalf("view missing running status:\n%s", view)
	}
	if strings.Count(view, status) != 1 {
		t.Fatalf("view should render running status once:\n%s", view)
	}
}

func serviceDoneFromStartCmd(t *testing.T, cmd tea.Cmd) serviceStoppedMsg {
	t.Helper()
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		done, ok := msg.(serviceStoppedMsg)
		if !ok {
			t.Fatalf("message type %T, want tea.BatchMsg or serviceStoppedMsg", msg)
		}
		return done
	}
	if len(batch) == 0 {
		t.Fatal("empty command batch")
	}
	msg = batch[0]()
	done, ok := msg.(serviceStoppedMsg)
	if !ok {
		t.Fatalf("batch[0] message type %T, want serviceStoppedMsg", msg)
	}
	return done
}

func requireModel(tb testing.TB, model tea.Model) *Model {
	tb.Helper()
	next, ok := model.(*Model)
	if !ok {
		tb.Fatalf("model type %T, want *Model", model)
	}
	return next
}

func requireNodeItem(tb testing.TB, item list.Item) nodeItem {
	tb.Helper()
	node, ok := item.(nodeItem)
	if !ok {
		tb.Fatalf("item type %T, want nodeItem", item)
	}
	return node
}

func testNode() x365.Node {
	return x365.Node{
		Name:      "test-node",
		UUID:      "00112233-4455-6677-8899-aabbccddeeff",
		Server:    "example.com",
		Port:      443,
		Path:      "/x365",
		Host:      "example.net",
		SNI:       "example.net",
		PublicKey: testRealityPublicKey,
		ShortID:   "01020304",
	}
}

type fakeService struct {
	done   chan struct{}
	once   sync.Once
	events []proxy.Event
	err    error
}

func newFakeService() *fakeService               { return &fakeService{done: make(chan struct{})} }
func (s *fakeService) Stop()                     { s.once.Do(func() { close(s.done) }) }
func (s *fakeService) Done() <-chan struct{}     { return s.done }
func (s *fakeService) Err() error                { return s.err }
func (s *fakeService) NextEvents() []proxy.Event { events := s.events; s.events = nil; return events }
