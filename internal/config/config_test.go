package config

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/rbee3u/efanadp/internal/x365"
)

func parseNodesText(text string) ([]x365.Node, error) {
	config, err := parseConfigText(text)
	if err != nil {
		return nil, err
	}
	return config.Nodes, nil
}

func parseConfigText(text string) (Config, error) {
	return parseConfigBytes([]byte(text))
}

const (
	testUserID           = "00112233-4455-6677-8899-aabbccddeeff"
	testRealityPublicKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
)

func testProxyURL(server, path string) string {
	return testProxyURLForUser(testUserID, server, path)
}

func testProxyURLForUser(userID, server, path string) string {
	return "x365://" + userID + "@" + server +
		":443?path=" + path +
		"&host=example.net&sni=example.net&pbk=" + testRealityPublicKey + "&sid=01020304"
}

func testProxyTemplateURL() string {
	return "x365://@example.com" +
		":443?path=/hk19" +
		"&host=example.net&sni=example.net&pbk=" + testRealityPublicKey + "&sid=01020304"
}

func testPackedProxyTemplate(t *testing.T, text string) string {
	t.Helper()

	tmpl, err := packProxyTemplate([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func requireRedactedError(t *testing.T, err error, want string, secrets ...string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err=%v, want %q", err, want)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("err=%q leaks %q", err, secret)
		}
	}
}

func TestParseNodesTextFromProxyYAML(t *testing.T) {
	nodes, err := parseNodesText(`
direct_domain:
  - example.cn
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
  Hong Kong20:
    - ` + testProxyURL("example.org", "/hk20") + `
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes)=%d, want 2", len(nodes))
	}
	if nodes[0].Name != "Hong Kong19" || nodes[0].Path != "/hk19" {
		t.Fatalf("bad first node: %+v", nodes[0])
	}
}

func TestParseConfigTextLoadsRuntimeSettings(t *testing.T) {
	config, err := parseConfigText(`
listen: 127.0.0.1:10866
log:
  level: debug
  output: stderr
  format: json
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`)
	if err != nil {
		t.Fatal(err)
	}
	if config.Listen != "127.0.0.1:10866" {
		t.Fatalf("listen=%q, want custom listen", config.Listen)
	}
	wantLog := LogConfig{Level: "debug", Output: "stderr", Format: "json"}
	if config.Log != wantLog {
		t.Fatalf("log=%+v, want %+v", config.Log, wantLog)
	}
	if len(config.Nodes) != 1 {
		t.Fatalf("len(nodes)=%d, want 1", len(config.Nodes))
	}
}

func TestParseConfigTextLoadsDefaultLogConfig(t *testing.T) {
	config, err := parseConfigText(`
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`)
	if err != nil {
		t.Fatal(err)
	}
	if config.Log != DefaultLogConfig() {
		t.Fatalf("log=%+v, want default %+v", config.Log, DefaultLogConfig())
	}
}

func TestParseConfigTextLoadsDefaultListen(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{
			name: "missing",
			text: `
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`,
		},
		{
			name: "blank",
			text: `
listen: "   "
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseConfigText(tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if config.Listen != DefaultListen {
				t.Fatalf("listen=%q, want default %q", config.Listen, DefaultListen)
			}
		})
	}
}

func TestParseConfigTextNormalizesLogConfig(t *testing.T) {
	config, err := parseConfigText(`
log:
  level: warning
  output: STDOUT
  format: TEXT
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`)
	if err != nil {
		t.Fatal(err)
	}
	want := LogConfig{Level: "warn", Output: "stdout", Format: "text"}
	if config.Log != want {
		t.Fatalf("log=%+v, want %+v", config.Log, want)
	}
}

func TestParseConfigTextRejectsInvalidLogConfig(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want string
	}{
		{
			name: "level",
			log:  "  level: trace\n",
			want: "invalid log.level",
		},
		{
			name: "output",
			log:  "  output: \"\"\n",
			want: "invalid log.output",
		},
		{
			name: "format",
			log:  "  format: yaml\n",
			want: "invalid log.format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConfigText(`
log:
` + tt.log + `
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseConfigTextRejectsLegacyLogPath(t *testing.T) {
	_, err := parseConfigText(`
log_path: logs/custom.log
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`)
	if err == nil || !strings.Contains(err.Error(), "log_path is no longer supported") {
		t.Fatalf("err=%v, want legacy log_path error", err)
	}
}

// A template is zlib-compressed base64 today and was plain base64 YAML before, so
// both must unpack and receive the UUID: a config that worked before an upgrade
// has to keep working. Anything that is neither is reported rather than treated
// as an empty node list, which would otherwise look like a config with no nodes.
func TestParseConfigTextUnpacksProxyTemplateEncodings(t *testing.T) {
	templateBody := `
Hong Kong19:
  - ` + testProxyTemplateURL() + `
`

	tests := []struct {
		name     string
		template string
		wantErr  string
	}{
		{
			name:     "zlib packed",
			template: testPackedProxyTemplate(t, templateBody),
		},
		{
			name:     "legacy plain base64",
			template: base64.StdEncoding.EncodeToString([]byte(templateBody)),
		},
		{
			name:     "neither is rejected",
			template: base64.StdEncoding.EncodeToString([]byte("not compressed template data")),
			wantErr:  "decompress proxies_tmpl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseConfigText(`
proxies_uuid: ` + testUserID + `
proxies_tmpl: ` + tt.template + `
`)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err=%v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(config.Nodes) != 1 {
				t.Fatalf("len(nodes)=%d, want 1", len(config.Nodes))
			}
			node := config.Nodes[0]
			if node.UUID != testUserID || node.Server != "example.com" {
				t.Fatalf("node=%+v, want UUID-filled template", node)
			}
		})
	}
}

func TestUnpackProxyTemplateRejectsOversizedData(t *testing.T) {
	oversized := strings.Repeat("x", maxProxyTemplateBytes+1)
	packed, err := packProxyTemplate([]byte(oversized))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		encoded string
	}{
		{name: "zlib expansion", encoded: packed},
		{name: "legacy base64", encoded: base64.StdEncoding.EncodeToString([]byte(oversized))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := unpackProxyTemplate(tt.encoded)
			if !errors.Is(err, errProxyTemplateTooLarge) {
				t.Fatalf("err=%v, want errProxyTemplateTooLarge", err)
			}
		})
	}
}

// Blank list entries are tolerated because hand-edited configs collect them, but
// a group that holds nothing else is reported rather than silently contributing
// no nodes. Both the direct proxies list and the unpacked template carry the same
// policy, and each names its own field so the message points at the right one.
func TestParseConfigTextSkipsBlankProxyURIs(t *testing.T) {
	tests := []struct {
		name string
		// build renders a config whose single group holds urls verbatim.
		build func(t *testing.T, urls string) string
		// validURL is the node URL form that path accepts: the direct list carries
		// the UUID, a template must omit it.
		validURL    string
		wantEmptyIs string
	}{
		{
			name: "proxies list",
			build: func(_ *testing.T, urls string) string {
				return "proxies:\n  Hong Kong19:\n" + urls
			},
			validURL:    testProxyURL("example.com", "/hk19"),
			wantEmptyIs: "empty proxy URL list",
		},
		{
			name: "packed template",
			build: func(t *testing.T, urls string) string {
				t.Helper()
				return "proxies_uuid: " + testUserID + "\n" +
					"proxies_tmpl: " + testPackedProxyTemplate(t, "Hong Kong19:\n"+urls) + "\n"
			},
			validURL:    testProxyTemplateURL(),
			wantEmptyIs: "empty proxy URL template list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A blank entry alongside a real one is skipped.
			config, err := parseConfigText(tt.build(t, `  - ""
  - "   "
  - `+tt.validURL+"\n"))
			if err != nil {
				t.Fatal(err)
			}
			if len(config.Nodes) != 1 {
				t.Fatalf("len(nodes)=%d, want 1", len(config.Nodes))
			}

			// A group of nothing but blanks is an error, not an empty node list.
			_, err = parseConfigText(tt.build(t, `  - ""
  - "   "
`))
			if err == nil || !strings.Contains(err.Error(), tt.wantEmptyIs) {
				t.Fatalf("err=%v, want %q", err, tt.wantEmptyIs)
			}
		})
	}
}

func TestParseConfigTextRequiresUUIDForProxyTemplate(t *testing.T) {
	tmpl := testPackedProxyTemplate(t, `
Hong Kong19:
  - `+testProxyTemplateURL()+`
`)
	_, err := parseConfigText(`proxies_tmpl: ` + tmpl + "\n")
	if err == nil || !strings.Contains(err.Error(), "missing proxies_uuid") {
		t.Fatalf("err=%v, want missing proxies_uuid", err)
	}
}

func TestParseConfigTextRejectsUUIDInProxyTemplate(t *testing.T) {
	tmpl := testPackedProxyTemplate(t, `
Hong Kong19:
  - `+testProxyURL("example.com", "/hk19")+`
`)
	_, err := parseConfigText(`
proxies_uuid: ` + testUserID + `
proxies_tmpl: ` + tmpl + `
`)
	if err == nil || !strings.Contains(err.Error(), "must omit UUID") {
		t.Fatalf("err=%v, want UUID template rejection", err)
	}
	if strings.Contains(err.Error(), "...") {
		t.Fatalf("err=%q should not end with punctuation", err)
	}
}

func TestParseConfigTextRedactsProxyURLValidationErrors(t *testing.T) {
	unsupportedProxyURL := "https://secret.example.org/hidden-node?" +
		"uuid=" + testUserID + "&pbk=test-public-key"
	malformedNodeURL := "x365://" + testUserID + "@secret%zz.example.org:443?" +
		"path=/hidden-node&host=secret.example.net&" +
		"sni=secret.example.net&pbk=test-public-key&sid=01020304"
	templateURL := "x365://" + testUserID + "@secret.example.org:443?" +
		"path=/hidden-node&host=secret.example.net&" +
		"sni=secret.example.net&pbk=test-public-key&sid=01020304"
	template := testPackedProxyTemplate(t, "test:\n  - "+templateURL+"\n")

	tests := []struct {
		name    string
		text    string
		want    string
		secrets []string
	}{
		{
			name: "unsupported proxy URL",
			text: "proxies:\n  test:\n    - " + unsupportedProxyURL + "\n",
			want: "unsupported proxy URL",
			secrets: []string{
				unsupportedProxyURL,
				"secret.example.org",
				"hidden-node",
				testUserID,
				"test-public-key",
			},
		},
		{
			name: "malformed runtime x365 URL",
			text: "proxies:\n  test:\n    - " + malformedNodeURL + "\n",
			want: "invalid x365 URL",
			secrets: []string{
				malformedNodeURL,
				"secret%zz.example.org",
				"secret.example.net",
				"hidden-node",
				testUserID,
				"test-public-key",
				"01020304",
			},
		},
		{
			name: "template includes uuid",
			text: "proxies_uuid: " + testUserID + "\n" +
				"proxies_tmpl: " + template + "\n",
			want: "x365 URL template must omit UUID",
			secrets: []string{
				"secret.example.org",
				"secret.example.net",
				"hidden-node",
				"test-public-key",
				"01020304",
			},
		},
		{
			name: "invalid node uuid",
			text: "proxies:\n  test:\n    - " + testProxyURLForUser("not-a-uuid", "secret.example.org", "/hidden-node") + "\n",
			want: "invalid user id",
			secrets: []string{
				"secret.example.org",
				"hidden-node",
				testRealityPublicKey,
				"01020304",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseConfigText(tt.text)
			requireRedactedError(t, err, tt.want, tt.secrets...)
		})
	}
}

// Load wraps the parse error with the config path, so it is the path that reaches
// the TUI. The redaction has to survive that wrapping: the file name is safe to
// show, the node material inside the file is not.
func TestLoadRejectsInvalidNodeUUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := `
proxies:
  test:
    - ` + testProxyURLForUser("not-a-uuid", "example.com", "/x") + `
`
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	requireRedactedError(t, err, "invalid user id", "example.com", testRealityPublicKey)
}

func TestConfigReferenceHasSafeTemplateStructure(t *testing.T) {
	data, err := os.ReadFile("../../config.reference.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var config struct {
		ProxiesUUID string `yaml:"proxies_uuid"`
		ProxiesTmpl string `yaml:"proxies_tmpl"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.ProxiesUUID != "<your-x365-uuid>" {
		t.Fatalf("proxies_uuid=%q, want placeholder", config.ProxiesUUID)
	}
	if strings.TrimSpace(config.ProxiesTmpl) == "" {
		t.Fatal("missing proxies_tmpl payload")
	}
	assertProxyTemplateLinesWrapped(t, string(data))
}

func TestParseNodesTextPreservesProxyOrder(t *testing.T) {
	nodes, err := parseNodesText(`
proxies:
  Vietnam01:
    - ` + testProxyURL("example.vn", "/vn") + `
  United States02:
    - ` + testProxyURL("example.us", "/us2") + `
  Japan01:
    - ` + testProxyURL("example.jp", "/jp") + `
  Singapore01:
    - ` + testProxyURL("example.sg", "/sg") + `
  Hong Kong02:
    - ` + testProxyURL("example.hk", "/hk2") + `
  United States01:
    - ` + testProxyURL("example.us", "/us1") + `
  Korea01:
    - ` + testProxyURL("example.kr", "/kr") + `
  Hong Kong01:
    - ` + testProxyURL("example.hk", "/hk1") + `
`)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Vietnam01", "United States02", "Japan01", "Singapore01", "Hong Kong02", "United States01", "Korea01", "Hong Kong01"}
	if len(nodes) != len(want) {
		t.Fatalf("len(nodes)=%d, want %d", len(nodes), len(want))
	}
	for i, name := range want {
		if nodes[i].Name != name {
			t.Fatalf("nodes[%d].Name=%q, want %q; got order=%v", i, nodes[i].Name, name, nodeNames(nodes))
		}
	}
}

func TestParseConfigTextLoadsShadowsocksSniffConfig(t *testing.T) {
	config, err := parseConfigText(`
sniff:
  shadowsocks:
    - name: landing
      server: 203.0.113.10
      port: 39281
      cipher: aes-256-gcm
      password: secret-password
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
`)
	if err != nil {
		t.Fatal(err)
	}
	nodes := config.Nodes
	if len(nodes) != 1 {
		t.Fatalf("len(nodes)=%d, want 1", len(nodes))
	}
	configs := config.ShadowsocksSniffConfigs
	if len(configs) != 1 {
		t.Fatalf("len(sniff configs)=%d, want 1", len(configs))
	}
	if configs[0].Method != "aes-256-gcm" || configs[0].Cipher != "" {
		t.Fatalf("method=%q cipher=%q, want normalized method", configs[0].Method, configs[0].Cipher)
	}
	if strings.Contains(nodes[0].Label(), "secret-password") {
		t.Fatalf("node label leaks shadowsocks password: %s", nodes[0].Label())
	}
}

func TestParseConfigTextKeepsShadowsocksSniffConfigGlobal(t *testing.T) {
	config, err := parseConfigText(`
sniff:
  shadowsocks:
    - name: landing
      server: 203.0.113.10
      port: 39281
      cipher: aes-256-gcm
      password: <shadowsocks-password>
proxies:
  Hong Kong19:
    - ` + testProxyURL("example.com", "/hk19") + `
  Hong Kong20:
    - ` + testProxyURL("example.org", "/hk20") + `
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Nodes) != 2 {
		t.Fatalf("len(nodes)=%d, want 2", len(config.Nodes))
	}
	if len(config.ShadowsocksSniffConfigs) != 1 {
		t.Fatalf("len(sniff configs)=%d, want 1", len(config.ShadowsocksSniffConfigs))
	}
}

func nodeNames(nodes []x365.Node) []string {
	names := make([]string, len(nodes))
	for i := range nodes {
		names[i] = nodes[i].Name
	}
	return names
}
