package config

import (
	"errors"
	"fmt"
	"strings"
)

// LogConfig controls the minimum level, encoding, and output destination.
type LogConfig struct {
	Level  string
	Format string
	Output string
}

// Defaults applied to omitted logging fields. DefaultLogOutput is relative to
// the working directory.
const (
	DefaultLogLevel  = "info"
	DefaultLogOutput = "logs/app.log"
	DefaultLogFormat = "text"
)

// DefaultLogConfig returns the logging policy used for omitted log fields.
func DefaultLogConfig() LogConfig {
	return LogConfig{
		Level:  DefaultLogLevel,
		Output: DefaultLogOutput,
		Format: DefaultLogFormat,
	}
}

type rawLogConfig struct {
	Level  *string `yaml:"level"`
	Format *string `yaml:"format"`
	Output *string `yaml:"output"`
}

func normalizeLogConfig(raw rawLogConfig) (LogConfig, error) {
	level, err := normalizeLogLevel(raw.Level)
	if err != nil {
		return LogConfig{}, err
	}
	output, err := normalizeLogOutput(raw.Output)
	if err != nil {
		return LogConfig{}, err
	}
	format, err := normalizeLogFormat(raw.Format)
	if err != nil {
		return LogConfig{}, err
	}
	return LogConfig{
		Level:  level,
		Format: format,
		Output: output,
	}, nil
}

func normalizeLogLevel(value *string) (string, error) {
	if value == nil {
		return DefaultLogLevel, nil
	}
	level := strings.ToLower(strings.TrimSpace(*value))
	if level == "" {
		return "", errors.New("invalid log.level: empty value")
	}
	if level == "warning" {
		level = "warn"
	}
	switch level {
	case "debug", "info", "warn", "error":
		return level, nil
	default:
		return "", fmt.Errorf("invalid log.level %q: expected debug, info, warn, warning, or error", *value)
	}
}

func normalizeLogOutput(value *string) (string, error) {
	if value == nil {
		return DefaultLogOutput, nil
	}
	output := strings.TrimSpace(*value)
	if output == "" {
		return "", errors.New("invalid log.output: empty value")
	}
	if strings.ContainsRune(output, 0) {
		return "", errors.New("invalid log.output: path contains NUL byte")
	}
	if strings.EqualFold(output, "stdout") {
		return "stdout", nil
	}
	if strings.EqualFold(output, "stderr") {
		return "stderr", nil
	}
	return output, nil
}

func normalizeLogFormat(value *string) (string, error) {
	if value == nil {
		return DefaultLogFormat, nil
	}
	format := strings.ToLower(strings.TrimSpace(*value))
	if format == "" {
		return "", errors.New("invalid log.format: empty value")
	}
	switch format {
	case "text", "json":
		return format, nil
	default:
		return "", fmt.Errorf("invalid log.format %q: expected text or json", *value)
	}
}
