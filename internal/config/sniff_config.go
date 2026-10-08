package config

import (
	"fmt"
	"strings"

	"github.com/rbee3u/efanadp/internal/sniff"
)

type rawShadowsocksSniffConfig struct {
	Name     string `yaml:"name"`
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	Method   string `yaml:"method"`
	Cipher   string `yaml:"cipher"`
	Password string `yaml:"password"`
}

func normalizeShadowsocksSniffConfigs(configs []rawShadowsocksSniffConfig) ([]sniff.ShadowsocksConfig, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	normalized := make([]sniff.ShadowsocksConfig, 0, len(configs))
	for i, raw := range configs {
		method := strings.TrimSpace(raw.Method)
		if method == "" {
			method = strings.TrimSpace(raw.Cipher)
		}
		config := sniff.ShadowsocksConfig{
			Name:     strings.TrimSpace(raw.Name),
			Server:   strings.TrimSpace(raw.Server),
			Port:     raw.Port,
			Method:   strings.ToLower(method),
			Password: raw.Password,
		}
		if config.Server == "" {
			return nil, fmt.Errorf("sniff.shadowsocks[%d]: missing server", i)
		}
		if config.Port <= 0 || config.Port > 65535 {
			return nil, fmt.Errorf("sniff.shadowsocks[%d]: invalid port", i)
		}
		if config.Method == "" {
			return nil, fmt.Errorf("sniff.shadowsocks[%d]: missing method", i)
		}
		if config.Password == "" {
			return nil, fmt.Errorf("sniff.shadowsocks[%d]: missing password", i)
		}
		normalized = append(normalized, config)
	}
	return normalized, nil
}
