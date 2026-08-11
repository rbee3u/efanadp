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
	path := filepath.Join(t.TempDir(), "settings.json")
	want := appSettings{ConfigPath: "proxy.yaml", NodeName: "test-node"}

	if err := saveSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("settings=%+v, want %+v", got, want)
	}
}

func TestNewRuntimeConfigUsesWorkingDirectorySettingsPath(t *testing.T) {
	runtime := newRuntimeConfig(Options{})
	if runtime.settingsPath != defaultSettingsPath {
		t.Fatalf("settings path=%q, want %q", runtime.settingsPath, defaultSettingsPath)
	}
}

func TestSaveSettingsFailureShowsWarning(t *testing.T) {
	settingsPath := t.TempDir()
	model := newTestModel(Options{})
	model.config.settingsPath = settingsPath
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

func TestSaveSettingsFailureWarnsAfterStatusWasCleared(t *testing.T) {
	settingsPath := t.TempDir()
	model := newTestModel(Options{})
	model.config.settingsPath = settingsPath
	model.config.configPath = "proxy.yaml"
	// A cleared error leaves statusKind behind; the warning is still the only
	// thing the user can see, so it has to be shown.
	model.setStatus(statusError, "Service exited")
	model.clearStatus()

	model.saveSettingsLogged()

	if model.statusKind != statusWarning {
		t.Fatalf("status kind=%v, want warning", model.statusKind)
	}
	if !strings.Contains(model.status, "Failed to save settings") {
		t.Fatalf("status=%q, want settings save warning", model.status)
	}
}

func TestSaveSettingsFailureKeepsVisibleError(t *testing.T) {
	settingsPath := t.TempDir()
	model := newTestModel(Options{})
	model.config.settingsPath = settingsPath
	model.config.configPath = "proxy.yaml"
	model.setStatus(statusError, "Service exited: boom")

	model.saveSettingsLogged()

	if model.status != "Service exited: boom" {
		t.Fatalf("status=%q, want the visible error kept", model.status)
	}
}

func TestResolveConfigPathPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		settings appSettings
		want     string
	}{
		{
			name:     "explicit flag wins",
			explicit: "flag.yaml",
			settings: appSettings{ConfigPath: "settings.yaml"},
			want:     "flag.yaml",
		},
		{
			name:     "settings fallback",
			settings: appSettings{ConfigPath: "settings.yaml"},
			want:     "settings.yaml",
		},
		{
			name:     "empty settings path uses default",
			settings: appSettings{ConfigPath: "  "},
			want:     "config.yaml",
		},
		{
			name: "missing settings uses default",
			want: "config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveConfigPath(tt.explicit, tt.settings)
			if got != tt.want {
				t.Fatalf("resolveConfigPath()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrepareStartupUsesSettingsConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	configPath := filepath.Join(dir, "proxy.yaml")
	writeStartupConfig(t, configPath)
	if err := saveSettings(defaultSettingsPath, appSettings{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}

	startup, err := prepareStartup(Options{})
	if err != nil {
		t.Fatal(err)
	}

	if startup.runtime.configPath != configPath {
		t.Fatalf("config path=%q, want %q", startup.runtime.configPath, configPath)
	}
}

func TestPrepareStartupUsesExplicitConfigPathOverSettings(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	explicitPath := filepath.Join(dir, "explicit.yaml")
	settingsConfigPath := filepath.Join(dir, "settings.yaml")
	writeStartupConfig(t, explicitPath)
	writeStartupConfig(t, settingsConfigPath)
	if err := saveSettings(defaultSettingsPath, appSettings{ConfigPath: settingsConfigPath}); err != nil {
		t.Fatal(err)
	}

	startup, err := prepareStartup(Options{ConfigPath: explicitPath})
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
	if err := os.Mkdir(defaultSettingsPath, 0o700); err != nil {
		t.Fatal(err)
	}

	startup, err := prepareStartup(Options{})
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
	runtime.loaded = config.Config{Nodes: []x365.Node{testNode()}}
	model := newModel(startupConfig{
		runtime:  runtime,
		settings: appSettings{ConfigPath: "proxy.yaml", NodeName: "test-node"},
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
