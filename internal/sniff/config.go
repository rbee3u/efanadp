// Package sniff inspects payloads for display-only target metadata. It never
// changes forwarding decisions or owns the carrier protocol.
package sniff

import (
	"net"
	"strings"
)

// ShadowsocksConfig identifies a landing server and its sniffing credentials.
// Values are normalized by the configuration loader and contain private material.
type ShadowsocksConfig struct {
	Name     string
	Server   string
	Port     int
	Method   string
	Password string
}

func matchingShadowsocksConfig(configs []ShadowsocksConfig, host string, port int) (ShadowsocksConfig, bool) {
	for _, config := range configs {
		if config.Port != port {
			continue
		}
		if sameHost(config.Server, host) {
			return config, true
		}
	}
	return ShadowsocksConfig{}, false
}

func sameHost(left, right string) bool {
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	if leftIP != nil || rightIP != nil {
		return leftIP != nil && rightIP != nil && leftIP.Equal(rightIP)
	}
	return strings.EqualFold(left, right)
}
