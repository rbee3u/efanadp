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

const defaultSettingsPath = "settings.json"

func loadSettings(path string) (appSettings, error) {
	if strings.TrimSpace(path) == "" {
		return appSettings{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return appSettings{}, nil
	}
	if err != nil {
		return appSettings{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return appSettings{}, nil
	}
	var settings appSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return appSettings{}, err
	}
	return settings, nil
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
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o600)
}

func (m *Model) saveSettings() error {
	settings := appSettings{
		ConfigPath: m.config.configPath,
		NodeName:   m.rememberedNodeName,
	}
	return saveSettings(m.config.settingsPath, settings)
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
