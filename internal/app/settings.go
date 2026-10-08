package app

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

	return writePrivateFileAtomically(path, ".settings-*.tmp", data)
}
