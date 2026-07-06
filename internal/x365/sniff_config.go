package x365

import (
	"fmt"
	"strings"
)

type ShadowsocksSniffConfig struct {
	Name     string `yaml:"name"`
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	Method   string `yaml:"method"`
	Cipher   string `yaml:"cipher"`
	Password string `yaml:"password"`
}

func NormalizeShadowsocksSniffConfigs(configs []ShadowsocksSniffConfig) ([]ShadowsocksSniffConfig, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	normalized := make([]ShadowsocksSniffConfig, 0, len(configs))
	for i, config := range configs {
		config.Name = strings.TrimSpace(config.Name)
		config.Server = strings.TrimSpace(config.Server)
		config.Method = strings.TrimSpace(config.Method)
		config.Cipher = strings.TrimSpace(config.Cipher)
		if config.Method == "" {
			config.Method = config.Cipher
		}
		config.Method = strings.ToLower(config.Method)
		config.Cipher = ""
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
