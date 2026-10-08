package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rbee3u/efanadp/internal/config"
	"github.com/rbee3u/efanadp/internal/session"
	"github.com/rbee3u/efanadp/internal/tui"
	"github.com/rbee3u/efanadp/internal/x365"
)

const defaultConfigPath = "config.yaml"

type startupConfig struct {
	configPath  string
	loaded      config.Config
	settings    appSettings
	settingsErr error
}

func prepareStartup(configPath string) (startupConfig, error) {
	settings, settingsErr := loadSettings(defaultSettingsPath)
	configPath = resolveConfigPath(configPath, settings)

	loaded, err := config.Load(configPath)
	if err != nil {
		return startupConfig{}, fmt.Errorf("load config: %s", userConfigLoadError(configPath, err))
	}
	if err := validateLogOutputPath(loaded.Log.Output, configPath, defaultSettingsPath); err != nil {
		return startupConfig{}, fmt.Errorf("load config: %w", err)
	}
	return startupConfig{
		configPath:  configPath,
		loaded:      loaded,
		settings:    settings,
		settingsErr: settingsErr,
	}, nil
}

// Prevent log appends from corrupting config or racing settings replacement.
func validateLogOutputPath(output, configPath, settingsPath string) error {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "stdout", "stderr":
		return nil
	}
	if pathsReferToSameFile(output, configPath) {
		return fmt.Errorf("log.output resolves to the config file")
	}
	if pathsReferToSameFile(output, settingsPath) {
		return fmt.Errorf("log.output resolves to the settings file")
	}
	return nil
}

func pathsReferToSameFile(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil && rightErr == nil && leftAbs == rightAbs {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func resolveConfigPath(explicitPath string, settings appSettings) string {
	if path := strings.TrimSpace(explicitPath); path != "" {
		return path
	}
	if path := strings.TrimSpace(settings.ConfigPath); path != "" {
		return path
	}
	return defaultConfigPath
}

func runInteractive(configPath string, run func(tui.Options) error) error {
	startup, err := prepareStartup(configPath)
	if err != nil {
		return err
	}
	logSink, err := newAppLogger(startup.loaded.Log)
	if err != nil {
		return err
	}
	defer func() { _ = logSink.Close() }()
	if startup.settingsErr != nil {
		logSink.Warnf("[efanadp] load settings failed path=%s err=%v", defaultSettingsPath, startup.settingsErr)
	}
	return run(tui.Options{
		ConfigPath:     startup.configPath,
		Listen:         startup.loaded.Listen,
		Nodes:          startup.loaded.Nodes,
		RememberedNode: startup.settings.NodeName,
		SettingsError:  startup.settingsErr,
		Logger:         logSink,
		Prepare: func(node x365.Node) (session.StartFunc, error) {
			return session.Prepare(node, session.Config{
				Listen:                  startup.loaded.Listen,
				Timeout:                 10 * time.Second,
				HandshakeTimeout:        15 * time.Second,
				ShadowsocksSniffConfigs: startup.loaded.ShadowsocksSniffConfigs,
				Logger:                  logSink,
			})
		},
		SaveSelection: func(nodeName string) error {
			err := saveSettings(defaultSettingsPath, appSettings{ConfigPath: startup.configPath, NodeName: nodeName})
			if err != nil {
				logSink.Warnf("[efanadp] save settings failed path=%s err=%v", defaultSettingsPath, err)
			}
			return err
		},
	})
}
