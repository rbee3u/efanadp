package tui

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/rbee3u/efanadp/internal/config"
)

type appLog interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type noopLogger struct{}

func (noopLogger) Infof(string, ...any)  {}
func (noopLogger) Warnf(string, ...any)  {}
func (noopLogger) Errorf(string, ...any) {}

type appLogger struct {
	logger *slog.Logger
	closer io.Closer
}

var newRotatingLogWriter = func(output string) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, err
	}
	return &lumberjack.Logger{
		Filename: output,
		MaxSize:  100,
	}, nil
}

func newAppLogger(cfg config.LogConfig) (*appLogger, error) {
	level, err := slogLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	writer, closer, err := logWriter(cfg.Output)
	if err != nil {
		return nil, err
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	default:
		handler = slog.NewTextHandler(writer, opts)
	}

	return &appLogger{logger: slog.New(handler), closer: closer}, nil
}

func slogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", value)
	}
}

func logWriter(output string) (io.Writer, io.Closer, error) {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "stdout":
		return os.Stdout, nil, nil
	case "stderr":
		return os.Stderr, nil, nil
	default:
		writer, err := newRotatingLogWriter(output)
		if err != nil {
			return nil, nil, err
		}
		return writer, writer, nil
	}
}

func (l *appLogger) Infof(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l *appLogger) Warnf(format string, args ...any) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

func (l *appLogger) Errorf(format string, args ...any) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

func (l *appLogger) Close() error {
	if l.closer == nil {
		return nil
	}
	err := l.closer.Close()
	l.closer = nil
	return err
}
