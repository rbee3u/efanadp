package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/rbee3u/efanadp/internal/config"
	"github.com/rbee3u/efanadp/internal/proxy"

	tea "github.com/charmbracelet/bubbletea"
)

type Options struct {
	ConfigPath       string
	SettingsPath     string
	Timeout          time.Duration
	HandshakeTimeout time.Duration
}

type modelDeps struct {
	logger     appLog
	serveSOCKS serveSOCKSFunc
}

type startupConfig struct {
	runtime        runtimeConfig
	loaded         config.Config
	settings       appSettings
	loadedSettings bool
	settingsErr    error
}

func Run(opts Options) error {
	startup, err := prepareStartup(opts)
	if err != nil {
		return err
	}
	logSink, err := newAppLogger(startup.runtime.log)
	if err != nil {
		return err
	}
	defer func() {
		_ = logSink.Close()
	}()
	model := newModel(startup, modelDeps{logger: logSink})
	model.saveSettingsLogged()
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}

func prepareStartup(opts Options) (startupConfig, error) {
	runtime := newRuntimeConfig(opts)
	settings, loadedSettings, settingsErr := loadSettings(runtime.settingsPath)
	runtime.configPath = resolveConfigPath(opts.ConfigPath, settings, loadedSettings)

	loaded, err := config.Load(runtime.configPath)
	if err != nil {
		return startupConfig{}, fmt.Errorf("load config: %s", userConfigLoadError(runtime.configPath, err))
	}
	applyLoadedRuntimeConfig(&runtime, loaded)
	return startupConfig{
		runtime:        runtime,
		loaded:         loaded,
		settings:       settings,
		loadedSettings: loadedSettings,
		settingsErr:    settingsErr,
	}, nil
}

func resolveConfigPath(explicitPath string, settings appSettings, loadedSettings bool) string {
	if path := strings.TrimSpace(explicitPath); path != "" {
		return path
	}
	if loadedSettings {
		if path := strings.TrimSpace(settings.ConfigPath); path != "" {
			return path
		}
	}
	return defaultConfigPath
}

func newModel(startup startupConfig, deps modelDeps) *Model {
	config := startup.runtime
	if deps.logger == nil {
		deps.logger = noopLogger{}
	}
	if deps.serveSOCKS == nil {
		deps.serveSOCKS = proxy.ServeSOCKS
	}

	nodes := nodesForDisplay(startup.loaded.Nodes)

	model := &Model{
		config:   config,
		deps:     deps,
		state:    stateNodeList,
		nodeList: newNodeList(nodeItemsWithCurrent(nodes, "")),
		nodes:    nodes,
		settings: startup.settings,
	}
	if startup.settingsErr != nil {
		deps.logger.Warnf("[efanadp] load settings failed path=%s err=%v", config.settingsPath, startup.settingsErr)
		model.setStatus(statusWarning, "Failed to read settings; using "+config.configPath+": "+shortError(startup.settingsErr))
	}
	if startup.loadedSettings && strings.TrimSpace(startup.settings.NodeName) != "" {
		model.rememberedNodeName = strings.TrimSpace(startup.settings.NodeName)
		if node, ok := model.selectNodeByName(model.rememberedNodeName); ok {
			model.setStatus(statusSuccess, "Restored previous node: "+node.Name)
			model.startupNode = &node
		} else if startup.settingsErr == nil {
			model.setStatus(statusWarning, fmt.Sprintf("Loaded %d nodes; previous node %q is not in this config", len(model.nodes), model.rememberedNodeName))
			model.rememberedNodeName = ""
		}
	}
	return model
}
