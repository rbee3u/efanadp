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
		"# Copy this file to config.yaml and keep it private.\n" +
		"# Replace proxies_uuid with your X365 account UUID; leave proxies_tmpl unchanged.\n\n"

	referenceSniff = "# Optional Shadowsocks target labels; forwarding is unchanged.\n" +
		"# Match the landing server by server/port and supply its method/password.\n" +
		"# sniff:\n" +
		"#   shadowsocks:\n" +
		"#     - name: landing\n" +
		"#       server: 203.0.113.10\n" +
		"#       port: 39281\n" +
		"#       method: aes-256-gcm\n" +
		"#       password: <shadowsocks-password>\n\n"

	// referenceTemplateValidationUUID stands in for stripped userinfo so a
	// UUID-less template URL can be parsed for validation. It never reaches the
	// generated file.
	referenceTemplateValidationUUID = "00112233-4455-6677-8899-aabbccddeeff"
)

// GenerateReference renders the committed reference template from a private
// config file's contents. It strips X365 UUID userinfo only: node hosts, paths,
// and REALITY material survive into the packed template, so callers must review
// the output before committing it. See docs/privacy.md.
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

	return renderReference(defaultListen(source.Listen), encoded), nil
}

func referenceProxyTemplate(source rawConfig) (yaml.MapSlice, error) {
	if len(source.Proxies) > 0 {
		if strings.TrimSpace(source.ProxiesTmpl) != "" {
			return nil, errors.New("proxies and proxies_tmpl cannot both be set")
		}
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
	return collectProxyURLs(value, ProxyListShape, proxyTemplateURL)
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
	return collectProxyURLs(value, TemplateListShape, uuidlessProxyTemplateURL)
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

func renderReference(listen, encodedProxies string) []byte {
	logConfig := DefaultLogConfig()

	var b strings.Builder
	b.WriteString(referenceIntro)
	b.WriteString("# listen is the local SOCKS5 TCP listen address; keep it on loopback (no-auth).\n")
	fmt.Fprintf(&b, "listen: %s\n", yamlScalar(listen))
	b.WriteString("\n")
	b.WriteString("# log controls runtime logging; see docs/configuration.md for accepted values.\n")
	b.WriteString("log:\n")
	fmt.Fprintf(&b, "  level: %s\n", yamlScalar(logConfig.Level))
	fmt.Fprintf(&b, "  output: %s\n", yamlScalar(logConfig.Output))
	fmt.Fprintf(&b, "  format: %s\n\n", yamlScalar(logConfig.Format))
	b.WriteString(referenceSniff)
	b.WriteString("proxies_uuid: <your-x365-uuid>\n")
	b.WriteString("\n")
	b.WriteString("# proxies_tmpl is zlib-compressed, base64-encoded YAML with UUID userinfo removed.\n")
	b.WriteString("# Hosts, paths, and REALITY material remain: packaging is not redaction.\n")
	b.WriteString("# Regenerate only for template maintenance; review output using docs/privacy.md.\n")
	b.WriteString("proxies_tmpl: |\n")
	for _, line := range wrapBase64Line(encodedProxies, 76) {
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
