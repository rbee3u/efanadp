package config

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestGenerateReferenceRemovesUUIDUserinfo(t *testing.T) {
	reference, err := GenerateReference([]byte(`
listen: 127.0.0.1:10866
log:
  level: debug
  output: /private/runtime/app.log
  format: json
proxies:
  test:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(reference)
	for _, want := range []string{
		"listen: 127.0.0.1:10866",
		"log:",
		"  level: info",
		"  output: logs/app.log",
		"  format: text",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("reference missing runtime setting %q", want)
		}
	}
	for _, unexpected := range []string{"log_path", "/private/runtime/app.log"} {
		if strings.Contains(text, unexpected) {
			t.Fatalf("reference contains unsafe log setting %q", unexpected)
		}
	}
	if strings.Contains(text, "00112233-4455-6677-8899-aabbccddeeff") {
		t.Fatal("reference leaks UUID")
	}
	for _, want := range []string{
		"Copy this file to config.yaml",
		"Replace proxies_uuid with your X365 account UUID",
		"zlib-compressed, base64-encoded YAML",
		"listen is the local SOCKS5 TCP listen address",
		"log controls runtime logging",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("reference missing comment %q", want)
		}
	}

	var config struct {
		ProxiesTmpl string `yaml:"proxies_tmpl"`
	}
	if err := yaml.Unmarshal(reference, &config); err != nil {
		t.Fatal(err)
	}
	decoded := decodePackedProxyTemplateForTest(t, config.ProxiesTmpl)
	decodedText := string(decoded)
	if strings.Contains(decodedText, "x365://00112233") {
		t.Fatal("template leaks UUID")
	}
	if !strings.Contains(decodedText, "x365://@example.com:443") {
		t.Fatal("template missing empty user marker")
	}
	assertProxyTemplateLinesWrapped(t, text)
}

func TestGenerateReferenceRejectsLegacyLogPath(t *testing.T) {
	_, err := GenerateReference([]byte(`
listen: 127.0.0.1:10866
log_path: logs/custom.log
proxies:
  test:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`))
	if err == nil || !strings.Contains(err.Error(), "log_path is no longer supported") {
		t.Fatalf("err=%v, want legacy log_path error", err)
	}
}

func TestGenerateReferenceUsesDefaultRuntimeSettings(t *testing.T) {
	reference, err := GenerateReference([]byte(`
proxies:
  test:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`))
	if err != nil {
		t.Fatal(err)
	}
	text := string(reference)
	for _, want := range []string{
		"listen: 127.0.0.1:10865",
		"log:",
		"  level: info",
		"  output: logs/app.log",
		"  format: text",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("reference missing default %q", want)
		}
	}
}

func TestGenerateReferenceRejectsMissingProxies(t *testing.T) {
	_, err := GenerateReference([]byte("listen: 127.0.0.1:10865\n"))
	if err == nil || !strings.Contains(err.Error(), "missing or empty proxies") {
		t.Fatalf("err=%v, want missing proxies error", err)
	}
}

func TestGenerateReferenceRedactsProxyURLValidationErrors(t *testing.T) {
	unsupportedProxyURL := "https://secret.example.org/hidden-node?" +
		"uuid=00112233-4455-6677-8899-aabbccddeeff&pbk=test-public-key"
	missingUUIDURL := "x365://@secret.example.org:443?" +
		"path=/hidden-node&host=secret.example.net&" +
		"sni=secret.example.net&pbk=test-public-key&sid=01020304"
	invalidUUIDURL := "x365://not-a-uuid@secret.example.org:443?" +
		"path=/hidden-node&host=secret.example.net&" +
		"sni=secret.example.net&pbk=test-public-key&sid=01020304"
	malformedURL := "x365://00112233-4455-6677-8899-aabbccddeeff@secret%zz.example.org:443?" +
		"path=/hidden-node&host=secret.example.net&sni=secret.example.net&pbk=test-public-key&sid=01020304"

	tests := []struct {
		name    string
		input   string
		want    string
		secrets []string
	}{
		{
			name:  "unsupported proxy URL",
			input: "proxies:\n  test:\n    - " + unsupportedProxyURL + "\n",
			want:  "unsupported proxy URL",
			secrets: []string{
				unsupportedProxyURL,
				"secret.example.org",
				"hidden-node",
				"00112233-4455-6677-8899-aabbccddeeff",
				"test-public-key",
			},
		},
		{
			name:  "missing UUID userinfo",
			input: "proxies:\n  test:\n    - " + missingUUIDURL + "\n",
			want:  "x365 URL does not contain UUID userinfo",
			secrets: []string{
				"secret.example.org",
				"secret.example.net",
				"hidden-node",
				"test-public-key",
				"01020304",
			},
		},
		{
			name:  "invalid UUID userinfo",
			input: "proxies:\n  test:\n    - " + invalidUUIDURL + "\n",
			want:  "invalid user id",
			secrets: []string{
				"not-a-uuid",
				"secret.example.org",
				"secret.example.net",
				"hidden-node",
				"test-public-key",
				"01020304",
			},
		},
		{
			name:  "malformed x365 URL",
			input: "proxies:\n  test:\n    - " + malformedURL + "\n",
			want:  "invalid x365 URL",
			secrets: []string{
				"secret.example.org",
				"secret.example.net",
				"hidden-node",
				"test-public-key",
				"01020304",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GenerateReference([]byte(tt.input))
			requireRedactedError(t, err, tt.want, tt.secrets...)
		})
	}
}

func TestGenerateReferenceRendersListenAsSafeYAMLScalar(t *testing.T) {
	reference, err := GenerateReference([]byte(`
listen: "127.0.0.1:10866\nproxies:\n  injected: true"
proxies:
  test:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`))
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(reference, &parsed); err != nil {
		t.Fatalf("generated reference is not valid YAML: %v", err)
	}
	if _, ok := parsed["proxies"]; ok {
		t.Fatal("generated reference has injected top-level proxies key")
	}
	if got, ok := parsed["listen"].(string); !ok || !strings.Contains(got, "proxies:\n  injected") {
		t.Fatalf("listen=%#v, want preserved scalar with embedded text", parsed["listen"])
	}
}

func TestGenerateReferenceRepacksExistingTemplate(t *testing.T) {
	first, err := GenerateReference([]byte(`
proxies:
  test:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateReference(first)
	if err != nil {
		t.Fatal(err)
	}

	var config struct {
		ProxiesTmpl string `yaml:"proxies_tmpl"`
	}
	if err := yaml.Unmarshal(second, &config); err != nil {
		t.Fatal(err)
	}
	decoded := string(decodePackedProxyTemplateForTest(t, config.ProxiesTmpl))
	if !strings.Contains(decoded, "x365://@example.com:443") {
		t.Fatal("repacked template missing URL template")
	}
}

func decodePackedProxyTemplateForTest(t *testing.T, encoded string) []byte {
	t.Helper()

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(decoded))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if err := zr.Close(); err != nil {
		t.Fatal(err)
	}
	return data
}
