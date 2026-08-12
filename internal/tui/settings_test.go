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
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
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
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("settings mode=%o, want 600", gotMode)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".settings-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary settings files left behind: %v", temps)
	}
}

func TestNewRuntimeConfigUsesWorkingDirectorySettingsPath(t *testing.T) {
	runtime := newRuntimeConfig(Options{})
	if runtime.settingsPath != defaultSettingsPath {
		t.Fatalf("settings path=%q, want %q", runtime.settingsPath, defaultSettingsPath)
	}
}

// A failed settings save costs the user their remembered node next launch, so it
// is worth surfacing, but not at the price of hiding a problem they have not read
// yet. The status line holds one message, so what is already on it decides:
// nothing or a benign message gives way, a visible error or warning does not.
// statusKind keeps its last value after clearStatus, so a cleared error must not
// be mistaken for one still on screen.
func TestSaveSettingsFailureWarningRespectsVisibleStatus(t *testing.T) {
	tests := []struct {
		name string
		// setup puts the model into the status state under test.
		setup       func(m *Model)
		wantWarning bool
		wantStatus  string
	}{
		{
			name:        "no status shows the warning",
			setup:       func(*Model) {},
			wantWarning: true,
		},
		{
			name:        "benign status gives way",
			setup:       func(m *Model) { m.setStatus(statusSuccess, "Loaded config") },
			wantWarning: true,
		},
		{
			name: "cleared error shows the warning",
			setup: func(m *Model) {
				m.setStatus(statusError, "Service exited")
				m.clearStatus()
			},
			wantWarning: true,
		},
		{
			name:       "visible error is kept",
			setup:      func(m *Model) { m.setStatus(statusError, "Service exited: boom") },
			wantStatus: "Service exited: boom",
		},
		{
			name:       "visible warning is kept",
			setup:      func(m *Model) { m.setStatus(statusWarning, "Node switch failed") },
			wantStatus: "Node switch failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newTestModel(Options{})
			// A directory is not writable as a file, so the save always fails.
			model.config.settingsPath = t.TempDir()
			model.config.configPath = "proxy.yaml"
			model.rememberedNodeName = "test-node"
			tt.setup(model)

			model.saveSettingsLogged()

			if !tt.wantWarning {
				if model.status != tt.wantStatus {
					t.Fatalf("status=%q, want the visible message kept: %q", model.status, tt.wantStatus)
				}
				return
			}
			if model.statusKind != statusWarning {
				t.Fatalf("status kind=%v, want warning", model.statusKind)
			}
			if !strings.Contains(model.status, "Failed to save settings") ||
				!strings.Contains(model.status, "may not restore automatically") {
				t.Fatalf("status=%q, want settings save warning", model.status)
			}
		})
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

func TestPrepareStartupRejectsLogOutputOverwritingRuntimeInput(t *testing.T) {
	tests := []struct {
		name       string
		configName string
		logOutput  string
		want       string
	}{
		{
			name:       "config file",
			configName: "proxy.yaml",
			logOutput:  "proxy.yaml",
			want:       "log.output resolves to the config file",
		},
		{
			name:       "settings file",
			configName: "proxy.yaml",
			logOutput:  defaultSettingsPath,
			want:       "log.output resolves to the settings file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			configText := `
log:
  output: ` + tt.logOutput + `
proxies:
  test-node:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x365&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`
			if err := os.WriteFile(tt.configName, []byte(configText), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := prepareStartup(Options{ConfigPath: tt.configName})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err=%v, want %q", err, tt.want)
			}
		})
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
