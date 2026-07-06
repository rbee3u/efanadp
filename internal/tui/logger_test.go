package tui

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rbee3u/efanadp/internal/config"
)

func TestAppLoggerWritesTextLogFile(t *testing.T) {
	usePlainLogFiles(t)

	path := filepath.Join(t.TempDir(), "nested", "app.log")
	logger, err := newAppLogger(config.LogConfig{
		Level:  "info",
		Output: path,
		Format: "text",
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Infof("hello %s", "text")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"level=INFO", "hello text"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text log missing %q:\n%s", want, text)
		}
	}
}

func TestAppLoggerWritesJSONLogFile(t *testing.T) {
	usePlainLogFiles(t)

	path := filepath.Join(t.TempDir(), "app.log")
	logger, err := newAppLogger(config.LogConfig{
		Level:  "info",
		Output: path,
		Format: "json",
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Warnf("hello %s", "json")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
		t.Fatalf("parse json log: %v\n%s", err, data)
	}
	if entry["msg"] != "hello json" {
		t.Fatalf("msg=%v, want hello json", entry["msg"])
	}
	if entry["level"] != "WARN" {
		t.Fatalf("level=%v, want WARN", entry["level"])
	}
}

func TestAppLoggerFiltersByLevel(t *testing.T) {
	usePlainLogFiles(t)

	path := filepath.Join(t.TempDir(), "app.log")
	logger, err := newAppLogger(config.LogConfig{
		Level:  "warn",
		Output: path,
		Format: "text",
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.Infof("hidden")
	logger.Errorf("visible")
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "hidden") {
		t.Fatalf("info log was not filtered:\n%s", text)
	}
	if !strings.Contains(text, "visible") {
		t.Fatalf("error log missing:\n%s", text)
	}
}

func usePlainLogFiles(t *testing.T) {
	t.Helper()
	original := newRotatingLogWriter
	newRotatingLogWriter = func(output string) (io.WriteCloser, error) {
		if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
			return nil, err
		}
		return os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	t.Cleanup(func() {
		newRotatingLogWriter = original
	})
}

func TestLogWriterSpecialOutputsDoNotCloseStdStreams(t *testing.T) {
	writer, closer, err := logWriter("stdout")
	if err != nil {
		t.Fatal(err)
	}
	if writer != os.Stdout || closer != nil {
		t.Fatalf("stdout writer=%T closer=%T, want os.Stdout and nil closer", writer, closer)
	}

	writer, closer, err = logWriter("stderr")
	if err != nil {
		t.Fatal(err)
	}
	if writer != os.Stderr || closer != nil {
		t.Fatalf("stderr writer=%T closer=%T, want os.Stderr and nil closer", writer, closer)
	}
}
