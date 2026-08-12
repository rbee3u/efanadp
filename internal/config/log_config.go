package config

import (
	"errors"
	"fmt"
	"strings"
)

type rawLogConfig struct {
	Level  *string `yaml:"level"`
	Format *string `yaml:"format"`
	Output *string `yaml:"output"`
}

func runtimeConfigFromRaw(raw rawConfig) (listen string, log LogConfig, err error) {
	if raw.LogPath != nil {
		return "", LogConfig{}, errors.New("log_path is no longer supported; use log.output")
	}

	logConfig, err := normalizeLogConfig(raw.Log)
	if err != nil {
		return "", LogConfig{}, err
	}

	return defaultListen(raw.Listen), logConfig, nil
}

func normalizeLogConfig(raw rawLogConfig) (LogConfig, error) {
	config := DefaultLogConfig()

	level, err := normalizeLogLevel(raw.Level)
	if err != nil {
		return LogConfig{}, err
	}
	config.Level = level

	output, err := normalizeLogOutput(raw.Output)
	if err != nil {
		return LogConfig{}, err
	}
	config.Output = output

	format, err := normalizeLogFormat(raw.Format)
	if err != nil {
		return LogConfig{}, err
	}
	config.Format = format

	return config, nil
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
