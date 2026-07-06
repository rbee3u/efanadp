package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rbee3u/efanadp/internal/config"
	"github.com/rbee3u/efanadp/internal/x365"
)

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".efanadp", "settings.json")
	want := appSettings{ConfigPath: "proxy.yaml", NodeName: "test-node"}

	if err := saveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := loadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected settings to load")
	}
	if got != want {
		t.Fatalf("settings=%+v, want %+v", got, want)
	}
}

func TestSaveSettingsFailureShowsWarning(t *testing.T) {
	settingsPath := t.TempDir()
	model := newTestModel(Options{SettingsPath: settingsPath})
	model.config.configPath = "proxy.yaml"
	model.rememberedNodeName = "test-node"
	model.setStatus(statusSuccess, "Loaded config")

	model.saveSettingsLogged()

	if model.statusKind != statusWarning {
		t.Fatalf("status kind=%v, want warning", model.statusKind)
	}
	if !strings.Contains(model.status, "Failed to save settings") || !strings.Contains(model.status, "may not restore automatically") {
		t.Fatalf("status=%q, want settings save warning", model.status)
	}
}

func TestResolveConfigPathPrecedence(t *testing.T) {
	tests := []struct {
		name           string
		explicit       string
		settings       appSettings
		loadedSettings bool
		want           string
	}{
		{
			name:           "explicit flag wins",
			explicit:       "flag.yaml",
			settings:       appSettings{ConfigPath: "settings.yaml"},
			loadedSettings: true,
			want:           "flag.yaml",
		},
		{
			name:           "settings fallback",
			settings:       appSettings{ConfigPath: "settings.yaml"},
			loadedSettings: true,
			want:           "settings.yaml",
		},
		{
			name:           "empty settings path uses default",
			settings:       appSettings{ConfigPath: "  "},
			loadedSettings: true,
			want:           "config.yaml",
		},
		{
			name: "missing settings uses default",
			want: "config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveConfigPath(tt.explicit, tt.settings, tt.loadedSettings)
			if got != tt.want {
				t.Fatalf("resolveConfigPath()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrepareStartupUsesSettingsConfigPath(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "proxy.yaml")
	writeStartupConfig(t, configPath)
	settingsPath := filepath.Join(dir, "settings.json")
	if err := saveSettings(settingsPath, appSettings{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}

	startup, err := prepareStartup(Options{SettingsPath: settingsPath})
	if err != nil {
		t.Fatal(err)
	}

	if startup.runtime.configPath != configPath {
		t.Fatalf("config path=%q, want %q", startup.runtime.configPath, configPath)
	}
	if !startup.loadedSettings {
		t.Fatal("expected settings to load")
	}
}

func TestPrepareStartupUsesExplicitConfigPathOverSettings(t *testing.T) {
	dir := t.TempDir()
	explicitPath := filepath.Join(dir, "explicit.yaml")
	settingsConfigPath := filepath.Join(dir, "settings.yaml")
	writeStartupConfig(t, explicitPath)
	writeStartupConfig(t, settingsConfigPath)
	settingsPath := filepath.Join(dir, "settings.json")
	if err := saveSettings(settingsPath, appSettings{ConfigPath: settingsConfigPath}); err != nil {
		t.Fatal(err)
	}

	startup, err := prepareStartup(Options{ConfigPath: explicitPath, SettingsPath: settingsPath})
	if err != nil {
		t.Fatal(err)
	}

	if startup.runtime.configPath != explicitPath {
		t.Fatalf("config path=%q, want %q", startup.runtime.configPath, explicitPath)
	}
}

func TestPrepareStartupFallsBackWhenSettingsReadFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeStartupConfig(t, "config.yaml")

	startup, err := prepareStartup(Options{SettingsPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	if startup.runtime.configPath != "config.yaml" {
		t.Fatalf("config path=%q, want config.yaml", startup.runtime.configPath)
	}
	if startup.settingsErr == nil {
		t.Fatal("expected settings read error")
	}
}

func TestNewModelRestoresSettingsNode(t *testing.T) {
	runtime := newRuntimeConfig(Options{ConfigPath: "proxy.yaml"})
	model := newModel(startupConfig{
		runtime:        runtime,
		loaded:         config.Config{Nodes: []x365.Node{testNode()}},
		settings:       appSettings{ConfigPath: "proxy.yaml", NodeName: "test-node"},
		loadedSettings: true,
	}, modelDeps{})

	if got := model.rememberedNodeName; got != "test-node" {
		t.Fatalf("remembered node=%q, want test-node", got)
	}
	if model.startupNode == nil || model.startupNode.Name != "test-node" {
		t.Fatalf("startup node=%+v, want test-node", model.startupNode)
	}
}

func writeStartupConfig(t *testing.T, path string) {
	t.Helper()
	err := os.WriteFile(path, []byte(`
proxies:
  test-node:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x365&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}
