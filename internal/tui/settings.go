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
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}

	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return nil
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
		if m.canShowSettingsWarning() {
			m.setStatus(statusWarning, "Failed to save settings; next launch may not restore automatically: "+shortError(err))
		}
	}
}

// canShowSettingsWarning keeps this warning from displacing a problem the user
// has not seen yet. statusKind keeps its last value after clearStatus, so an
// empty status means nothing is on screen to preserve.
func (m *Model) canShowSettingsWarning() bool {
	if m.status == "" {
		return true
	}
	return m.statusKind != statusError && m.statusKind != statusWarning
}
