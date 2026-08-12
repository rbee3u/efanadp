package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/rbee3u/efanadp/internal/x365"
)

const (
	referenceIntro = "# efanadp reference configuration.\n" +
		"# Copy this file to config.yaml before first use. Keep config.yaml private.\n" +
		"# Replace proxies_uuid with your X365 account UUID; leave proxies_tmpl unchanged\n" +
		"# unless you regenerate this file from a decrypted private config.\n\n"

	referenceSniff = "# sniff enables optional display-only target sniffing when efanadp first\n" +
		"# connects to a Shadowsocks landing server before the real target. Leave this\n" +
		"# block commented out unless your route has that first hop.\n" +
		"#\n" +
		"# shadowsocks entries are matched by server and port. method is the cipher\n" +
		"# name used by the landing server, and password is that Shadowsocks password.\n" +
		"# sniffing affects logs and TUI target labels only; relay behavior is unchanged.\n" +
		"# sniff:\n" +
		"#   shadowsocks:\n" +
		"#     - name: landing\n" +
		"#       server: 203.0.113.10\n" +
		"#       port: 39281\n" +
		"#       method: aes-256-gcm\n" +
		"#       password: <shadowsocks-password>\n\n"

	referenceTemplateValidationUUID = "00112233-4455-6677-8899-aabbccddeeff"
)

type referenceData struct {
	listen         string
	encodedProxies string
}

func GenerateReference(data []byte) ([]byte, error) {
	source, err := readRawConfig(data)
	if err != nil {
		return nil, err
	}
	if _, _, err := runtimeConfigFromRaw(source); err != nil {
		return nil, err
	}

	tmpl, err := referenceProxyTemplate(source)
	if err != nil {
		return nil, err
	}
	tmplYAML, err := yaml.Marshal(tmpl)
	if err != nil {
		return nil, fmt.Errorf("marshal proxy template: %w", err)
	}
	encoded, err := packProxyTemplate(tmplYAML)
	if err != nil {
		return nil, err
	}

	return renderReference(referenceData{
		listen:         defaultListen(source.Listen),
		encodedProxies: encoded,
	}), nil
}

func referenceProxyTemplate(source rawConfig) (yaml.MapSlice, error) {
	if len(source.Proxies) > 0 {
		return proxyTemplate(source.Proxies)
	}
	if strings.TrimSpace(source.ProxiesTmpl) != "" {
		return proxyTemplateFromPacked(source.ProxiesTmpl)
	}
	return nil, errors.New("missing or empty proxies field")
}

func proxyTemplate(proxies yaml.MapSlice) (yaml.MapSlice, error) {
	return mapProxyGroups(proxies, proxyTemplateURLs)
}

func proxyTemplateURLs(value any) ([]string, error) {
	return collectProxyURLs(value, "x365 URLs", "empty proxy URL list", proxyTemplateURL)
}

func proxyTemplateFromPacked(encoded string) (yaml.MapSlice, error) {
	data, err := unpackProxyTemplate(encoded)
	if err != nil {
		return nil, err
	}
	var source yaml.MapSlice
	if err := yaml.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("parse proxies_tmpl YAML: %s", safeYAMLError(err))
	}
	if len(source) == 0 {
		return nil, errors.New("proxies_tmpl contains no proxy groups")
	}

	return mapProxyGroups(source, uuidlessProxyTemplateURLs)
}

func uuidlessProxyTemplateURLs(value any) ([]string, error) {
	return collectProxyURLs(
		value,
		"x365 URL templates",
		"empty proxy URL template list",
		uuidlessProxyTemplateURL,
	)
}

func uuidlessProxyTemplateURL(raw string) (string, error) {
	filled, err := fillProxyTemplateURL(raw, referenceTemplateValidationUUID)
	if err != nil {
		return "", err
	}
	if _, err := x365.ParseNode("", filled); err != nil {
		return "", err
	}
	return raw, nil
}

func proxyTemplateURL(raw string) (string, error) {
	if !strings.HasPrefix(raw, "x365://") {
		return "", errors.New("unsupported proxy URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid x365 URL")
	}
	if u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
		return "", errors.New("x365 URL does not contain UUID userinfo")
	}
	if _, err := x365.ParseUserID(u.User.Username()); err != nil {
		return "", fmt.Errorf("invalid user id: %w", err)
	}
	if _, err := x365.ParseNode("", raw); err != nil {
		return "", err
	}
	u.User = url.User("")
	return u.String(), nil
}

func renderReference(reference referenceData) []byte {
	logConfig := DefaultLogConfig()

	var b strings.Builder
	b.WriteString(referenceIntro)
	b.WriteString("# listen is the local SOCKS5 TCP listen address for mihomo or direct tests.\n")
	b.WriteString("# Change the port only if 10865 is already in use or your mihomo rule points\n")
	b.WriteString("# at a different local port.\n")
	fmt.Fprintf(&b, "listen: %s\n", yamlScalar(reference.listen))
	b.WriteString("\n")
	b.WriteString("# log controls runtime logging. Use level debug only while troubleshooting.\n")
	b.WriteString("# output accepts stdout, stderr, or a local file path. File outputs rotate.\n")
	b.WriteString("# format accepts text for humans or json for structured log collectors.\n")
	b.WriteString("log:\n")
	fmt.Fprintf(&b, "  level: %s\n", yamlScalar(logConfig.Level))
	fmt.Fprintf(&b, "  output: %s\n", yamlScalar(logConfig.Output))
	fmt.Fprintf(&b, "  format: %s\n\n", yamlScalar(logConfig.Format))
	b.WriteString(referenceSniff)
	b.WriteString("# proxies_uuid is your X365 account UUID. It is inserted into every URL in\n")
	b.WriteString("# proxies_tmpl at startup. Replace the placeholder with the UUID from your\n")
	b.WriteString("# decrypted private config, and do not commit the filled config.yaml.\n")
	b.WriteString("proxies_uuid: <your-x365-uuid>\n")
	b.WriteString("\n")
	b.WriteString("# proxies_tmpl is zlib-compressed, base64-encoded YAML containing x365://\n")
	b.WriteString("# node URL templates with the UUID userinfo removed as x365://@server...\n")
	b.WriteString("# Treat it as packaging, not redaction: unpacked templates can still contain\n")
	b.WriteString("# provider hosts, paths, REALITY public keys, and short IDs. Do not edit this\n")
	b.WriteString("# block by hand; regenerate config.reference.yaml from a private config when\n")
	b.WriteString("# the node set changes.\n")
	b.WriteString("proxies_tmpl: |\n")
	for _, line := range wrapBase64Line(reference.encodedProxies, 76) {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return []byte(b.String())
}

func yamlScalar(value string) string {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%q", value)
	}
	return strings.TrimSuffix(string(data), "\n")
}

func wrapBase64Line(text string, width int) []string {
	if width <= 0 || len(text) <= width {
		return []string{text}
	}
	lines := make([]string, 0, (len(text)+width-1)/width)
	for len(text) > width {
		lines = append(lines, text[:width])
		text = text[width:]
	}
	if text != "" {
		lines = append(lines, text)
	}
	return lines
}
