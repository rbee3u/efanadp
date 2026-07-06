package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type appSettings struct {
	ConfigPath string `json:"configPath,omitempty"`
	NodeName   string `json:"nodeName,omitempty"`
}

func DefaultSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".efanadp", "settings.json")
	}
	return filepath.Join(home, ".efanadp", "settings.json")
}

func loadSettings(path string) (appSettings, bool, error) {
	if strings.TrimSpace(path) == "" {
		return appSettings{}, false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return appSettings{}, false, nil
	}
	if err != nil {
		return appSettings{}, false, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return appSettings{}, false, nil
	}
	var settings appSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return appSettings{}, false, err
	}
	return settings, true, nil
}

func saveSettings(path string, settings appSettings) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (m *Model) saveSettings() error {
	m.settings = appSettings{
		ConfigPath: m.config.configPath,
		NodeName:   m.rememberedNodeName,
	}
	return saveSettings(m.config.settingsPath, m.settings)
}

func (m *Model) saveSettingsLogged() {
	if err := m.saveSettings(); err != nil {
		m.logWarnf("[efanadp] save settings failed path=%s err=%v", m.config.settingsPath, err)
		if m.statusKind != statusError && m.statusKind != statusWarning {
			m.setStatus(statusWarning, "Failed to save settings; next launch may not restore automatically: "+shortError(err))
		}
	}
}

func shortError(err error) string {
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "unknown error"
	}
	if len([]rune(text)) <= 96 {
		return text
	}
	runes := []rune(text)
	return string(runes[:96]) + "…"
}
