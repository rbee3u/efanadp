package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/rbee3u/efanadp/internal/config"
)

func TestAppLoggerWritesTextLogFile(t *testing.T) {
	text := string(writeAppLog(t, config.LogConfig{
		Level:  "info",
		Output: filepath.Join(t.TempDir(), "nested", "app.log"),
		Format: "text",
	}, func(logger *appLogger) {
		logger.Infof("hello %s", "text")
	}))

	for _, want := range []string{"level=INFO", "hello text"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text log missing %q:\n%s", want, text)
		}
	}
}

func TestAppLoggerWritesJSONLogFile(t *testing.T) {
	data := writeAppLog(t, config.LogConfig{
		Level:  "info",
		Output: filepath.Join(t.TempDir(), "app.log"),
		Format: "json",
	}, func(logger *appLogger) {
		logger.Warnf("hello %s", "json")
	})

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
	text := string(writeAppLog(t, config.LogConfig{
		Level:  "warn",
		Output: filepath.Join(t.TempDir(), "app.log"),
		Format: "text",
	}, func(logger *appLogger) {
		logger.Infof("hidden")
		logger.Errorf("visible")
	}))

	if strings.Contains(text, "hidden") {
		t.Fatalf("info log was not filtered:\n%s", text)
	}
	if !strings.Contains(text, "visible") {
		t.Fatalf("error log missing:\n%s", text)
	}
}

// Use a plain file so log bytes remain at a known path without rotation.
func writeAppLog(t *testing.T, cfg config.LogConfig, write func(*appLogger)) []byte {
	t.Helper()
	usePlainLogFiles(t)

	logger, err := newAppLogger(cfg)
	if err != nil {
		t.Fatal(err)
	}
	write(logger)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(cfg.Output)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Zero MaxBackups and MaxAge would disable lumberjack's retention limits.
func TestRotatingLogWriterBoundsRetainedFiles(t *testing.T) {
	writer, err := newRotatingLogWriter(filepath.Join(t.TempDir(), "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = writer.Close()
	}()

	rotating, ok := writer.(*lumberjack.Logger)
	if !ok {
		t.Fatalf("writer type=%T, want *lumberjack.Logger", writer)
	}
	if rotating.MaxSize <= 0 {
		t.Fatalf("MaxSize=%d, want a positive rotation size", rotating.MaxSize)
	}
	if rotating.MaxBackups <= 0 {
		t.Fatalf("MaxBackups=%d, want a bound on retained files", rotating.MaxBackups)
	}
	if rotating.MaxAge <= 0 {
		t.Fatalf("MaxAge=%d, want a bound on retained age", rotating.MaxAge)
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
	tests := []struct {
		output string
		want   *os.File
	}{
		{output: "stdout", want: os.Stdout},
		{output: "stderr", want: os.Stderr},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			writer, closer, err := logWriter(tt.output)
			if err != nil {
				t.Fatal(err)
			}
			if writer != tt.want {
				t.Fatalf("%s writer=%T, want %s", tt.output, writer, tt.output)
			}
			if closer != nil {
				t.Fatalf("%s closer=%T, want nil so the stream is not closed", tt.output, closer)
			}
		})
	}
}
