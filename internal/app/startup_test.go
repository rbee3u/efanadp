package app

import (
	"errors"
	"io"
	"testing"

	"github.com/rbee3u/efanadp/internal/tui"
)

type trackedLogWriter struct{ closed bool }

func (w *trackedLogWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *trackedLogWriter) Close() error                { w.closed = true; return nil }

func TestInteractiveWiresStartupPersistenceAndOwnsLogger(t *testing.T) {
	t.Chdir(t.TempDir())
	writeStartupConfig(t, "proxy.yaml")
	if err := saveSettings(defaultSettingsPath, appSettings{ConfigPath: "proxy.yaml", NodeName: "test-node"}); err != nil {
		t.Fatal(err)
	}
	writer := &trackedLogWriter{}
	original := newRotatingLogWriter
	newRotatingLogWriter = func(string) (io.WriteCloser, error) { return writer, nil }
	t.Cleanup(func() { newRotatingLogWriter = original })
	boom := errors.New("terminal failure")
	err := runInteractive("", func(opts tui.Options) error {
		if writer.closed || opts.ConfigPath != "proxy.yaml" || opts.RememberedNode != "test-node" || len(opts.Nodes) != 1 || opts.Prepare == nil || opts.Logger == nil {
			t.Fatal("startup dependencies not wired")
		}
		// Preparation is offline. Starting the returned service is a separate action.
		if _, err := opts.Prepare(opts.Nodes[0]); err != nil {
			t.Fatal(err)
		}
		if err := opts.SaveSelection("next-node"); err != nil {
			t.Fatal(err)
		}
		return boom
	})
	if !errors.Is(err, boom) || !writer.closed {
		t.Fatalf("err=%v closed=%v", err, writer.closed)
	}
	saved, err := loadSettings(defaultSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConfigPath != "proxy.yaml" || saved.NodeName != "next-node" {
		t.Fatalf("saved=%+v", saved)
	}
}
