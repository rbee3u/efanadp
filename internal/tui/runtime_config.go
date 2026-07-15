package tui

import (
	"strings"
	"time"

	"github.com/rbee3u/efanadp/internal/config"
)

const (
	defaultConfigPath = "config.yaml"
)

type runtimeConfig struct {
	configPath       string
	loaded           config.Config
	settingsPath     string
	timeout          time.Duration
	handshakeTimeout time.Duration
}

func newRuntimeConfig(opts Options) runtimeConfig {
	return runtimeConfig{
		configPath:       strings.TrimSpace(opts.ConfigPath),
		loaded:           defaultLoadedConfig(),
		settingsPath:     defaultSettingsPath,
		timeout:          opts.Timeout,
		handshakeTimeout: opts.HandshakeTimeout,
	}
}

func defaultLoadedConfig() config.Config {
	return config.Config{
		Listen: config.DefaultListen,
		Log:    config.DefaultLogConfig(),
	}
}
