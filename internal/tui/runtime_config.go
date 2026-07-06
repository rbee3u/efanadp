package tui

import (
	"strings"
	"time"

	"github.com/rbee3u/efanadp/internal/config"
	"github.com/rbee3u/efanadp/internal/x365"
)

const (
	defaultConfigPath = "config.yaml"
)

type runtimeConfig struct {
	configPath       string
	listen           string
	log              config.LogConfig
	sniffConfigs     []x365.ShadowsocksSniffConfig
	settingsPath     string
	timeout          time.Duration
	handshakeTimeout time.Duration
}

func newRuntimeConfig(opts Options) runtimeConfig {
	return runtimeConfig{
		configPath:       strings.TrimSpace(opts.ConfigPath),
		listen:           config.DefaultListen,
		log:              config.DefaultLogConfig(),
		settingsPath:     strings.TrimSpace(opts.SettingsPath),
		timeout:          opts.Timeout,
		handshakeTimeout: opts.HandshakeTimeout,
	}
}

func applyLoadedRuntimeConfig(runtime *runtimeConfig, loaded config.Config) {
	runtime.listen = loaded.Listen
	runtime.log = loaded.Log
	runtime.sniffConfigs = loaded.ShadowsocksSniffConfigs
}
