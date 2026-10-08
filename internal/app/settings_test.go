package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	startup, err := prepareStartup("")
	if err != nil {
		t.Fatal(err)
	}

	if startup.configPath != configPath {
		t.Fatalf("config path=%q, want %q", startup.configPath, configPath)
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

	startup, err := prepareStartup(explicitPath)
	if err != nil {
		t.Fatal(err)
	}

	if startup.configPath != explicitPath {
		t.Fatalf("config path=%q, want %q", startup.configPath, explicitPath)
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

			_, err := prepareStartup(tt.configName)
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

	startup, err := prepareStartup("")
	if err != nil {
		t.Fatal(err)
	}

	if startup.configPath != "config.yaml" {
		t.Fatalf("config path=%q, want config.yaml", startup.configPath)
	}
	if startup.settingsErr == nil {
		t.Fatal("expected settings read error")
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
