// Package config parses runtime YAML and generates UUID-stripped reference
// templates. Parsed account and provider values are private; omit them from
// diagnostics.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/rbee3u/efanadp/internal/sniff"
	"github.com/rbee3u/efanadp/internal/x365"
)

// Config is one runtime snapshot. Nodes and sniffing entries contain private
// account or provider material and must not be included in public diagnostics.
type Config struct {
	Listen                  string
	Log                     LogConfig
	Nodes                   []x365.Node
	ShadowsocksSniffConfigs []sniff.ShadowsocksConfig
}

// DefaultListen stays on loopback because the adapter serves SOCKS5 no-auth only.
const DefaultListen = "127.0.0.1:10865"

// Load reads and validates one runtime configuration file. YAML errors retain
// their reason and position but omit source excerpts that may contain private
// node material.
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
		Shadowsocks []rawShadowsocksSniffConfig `yaml:"shadowsocks"`
	} `yaml:"sniff"`
}

func readRawConfig(data []byte) (rawConfig, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var raw rawConfig
	if err := decoder.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return rawConfig{}, configError(InvalidYAML, fmt.Errorf("parse YAML: %s", safeYAMLError(err)))
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return rawConfig{}, errors.New("multiple YAML documents are not supported")
	} else if !errors.Is(err, io.EOF) {
		return rawConfig{}, configError(InvalidYAML, fmt.Errorf("parse YAML: %s", safeYAMLError(err)))
	}
	return raw, nil
}

// Omit YAML source excerpts because they may contain private node material.
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

	sniffConfigs, err := normalizeShadowsocksSniffConfigs(raw.Sniff.Shadowsocks)
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
		return nil, configError(NoNodes, errors.New("proxies field contains no x365 nodes"))
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
