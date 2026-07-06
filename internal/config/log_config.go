package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

func runtimeConfigFromRaw(raw rawConfig) (string, LogConfig, error) {
	if raw.LogPath != nil {
		return "", LogConfig{}, errors.New("log_path is no longer supported; use log.output")
	}

	rawLog, presence := rawLogConfigFromMap(raw.Log)
	logConfig, err := normalizeLogConfig(rawLog, presence)
	if err != nil {
		return "", LogConfig{}, err
	}

	return defaultListen(raw.Listen), logConfig, nil
}

func rawLogConfigFromMap(items yaml.MapSlice) (LogConfig, logFieldPresence) {
	var raw LogConfig
	var presence logFieldPresence
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			continue
		}
		value := rawConfigString(item.Value)
		switch key {
		case "level":
			presence.level = true
			raw.Level = value
		case "format":
			presence.format = true
			raw.Format = value
		case "output":
			presence.output = true
			raw.Output = value
		}
	}
	return raw, presence
}

func rawConfigString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func normalizeLogConfig(raw LogConfig, presence logFieldPresence) (LogConfig, error) {
	config := DefaultLogConfig()

	level, err := normalizeLogLevel(raw.Level, presence.level)
	if err != nil {
		return LogConfig{}, err
	}
	config.Level = level

	output, err := normalizeLogOutput(raw.Output, presence.output)
	if err != nil {
		return LogConfig{}, err
	}
	config.Output = output

	format, err := normalizeLogFormat(raw.Format, presence.format)
	if err != nil {
		return LogConfig{}, err
	}
	config.Format = format

	return config, nil
}

func normalizeLogLevel(value string, set bool) (string, error) {
	level := strings.ToLower(strings.TrimSpace(value))
	if level == "" {
		if set {
			return "", errors.New("invalid log.level: empty value")
		}
		return DefaultLogLevel, nil
	}
	if level == "warning" {
		level = "warn"
	}
	switch level {
	case "debug", "info", "warn", "error":
		return level, nil
	default:
		return "", fmt.Errorf("invalid log.level %q: expected debug, info, warn, warning, or error", value)
	}
}

func normalizeLogOutput(value string, set bool) (string, error) {
	output := strings.TrimSpace(value)
	if output == "" {
		if set {
			return "", errors.New("invalid log.output: empty value")
		}
		return DefaultLogOutput, nil
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

func normalizeLogFormat(value string, set bool) (string, error) {
	format := strings.ToLower(strings.TrimSpace(value))
	if format == "" {
		if set {
			return "", errors.New("invalid log.format: empty value")
		}
		return DefaultLogFormat, nil
	}
	switch format {
	case "text", "json":
		return format, nil
	default:
		return "", fmt.Errorf("invalid log.format %q: expected text or json", value)
	}
}
