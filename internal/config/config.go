package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/rbee3u/efanadp/internal/x365"
)

type Config struct {
	Listen                  string
	Log                     LogConfig
	Nodes                   []x365.Node
	ShadowsocksSniffConfigs []x365.ShadowsocksSniffConfig
}

type LogConfig struct {
	Level  string
	Format string
	Output string
}

const (
	DefaultListen    = "127.0.0.1:10865"
	DefaultLogLevel  = "info"
	DefaultLogOutput = "logs/app.log"
	DefaultLogFormat = "text"
)

func DefaultLogConfig() LogConfig {
	return LogConfig{
		Level:  DefaultLogLevel,
		Output: DefaultLogOutput,
		Format: DefaultLogFormat,
	}
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	config, err := parseConfigBytes(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

type rawConfig struct {
	Listen      string        `yaml:"listen"`
	Log         rawLogConfig  `yaml:"log"`
	LogPath     *string       `yaml:"log_path"`
	Proxies     yaml.MapSlice `yaml:"proxies"`
	ProxiesTmpl string        `yaml:"proxies_tmpl"`
	ProxiesUUID string        `yaml:"proxies_uuid"`
	Sniff       struct {
		Shadowsocks []x365.ShadowsocksSniffConfig `yaml:"shadowsocks"`
	} `yaml:"sniff"`
}

func readRawConfig(data []byte) (rawConfig, error) {
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return rawConfig{}, fmt.Errorf("parse YAML: %s", safeYAMLError(err))
	}
	return raw, nil
}

// safeYAMLError keeps the parser's reason and source position without its
// default source excerpt. Config lines can contain account and node material,
// so returning the excerpt would leak private values through TUI or CLI errors.
func safeYAMLError(err error) string {
	return strings.TrimSpace(yaml.FormatError(err, false, false))
}

func parseConfigBytes(data []byte) (Config, error) {
	raw, err := readRawConfig(data)
	if err != nil {
		return Config{}, err
	}

	listen, logConfig, err := runtimeConfigFromRaw(raw)
	if err != nil {
		return Config{}, err
	}

	sniffConfigs, err := x365.NormalizeShadowsocksSniffConfigs(raw.Sniff.Shadowsocks)
	if err != nil {
		return Config{}, err
	}

	proxies, err := proxyGroupsFromConfig(
		raw.Proxies,
		raw.ProxiesTmpl,
		raw.ProxiesUUID,
	)
	if err != nil {
		return Config{}, err
	}
	nodes, err := parseProxyNodes(proxies)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Listen:                  listen,
		Log:                     logConfig,
		Nodes:                   nodes,
		ShadowsocksSniffConfigs: sniffConfigs,
	}, nil
}

func parseProxyNodes(proxies yaml.MapSlice) ([]x365.Node, error) {
	nodes := make([]x365.Node, 0, len(proxies))
	for _, item := range proxies {
		name := fmt.Sprint(item.Key)
		urls, err := parseProxyURLs(item.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for idx, rawURL := range urls {
			nodeName := name
			if len(urls) > 1 {
				nodeName = fmt.Sprintf("%s #%d", name, idx+1)
			}
			node, err := x365.ParseNode(nodeName, rawURL)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", nodeName, err)
			}
			nodes = append(nodes, node)
		}
	}
	if len(nodes) == 0 {
		return nil, errors.New("proxies field contains no x365 nodes")
	}
	return nodes, nil
}

func defaultListen(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultListen
	}
	return value
}
