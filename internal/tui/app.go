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
	Timeout          time.Duration
	HandshakeTimeout time.Duration
}

type modelDeps struct {
	logger     appLog
	serveSOCKS serveSOCKSFunc
}

type startupConfig struct {
	runtime     runtimeConfig
	settings    appSettings
	settingsErr error
}

func Run(opts Options) error {
	startup, err := prepareStartup(opts)
	if err != nil {
		return err
	}
	logSink, err := newAppLogger(startup.runtime.loaded.Log)
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
	settings, settingsErr := loadSettings(runtime.settingsPath)
	runtime.configPath = resolveConfigPath(runtime.configPath, settings)

	loaded, err := config.Load(runtime.configPath)
	if err != nil {
		return startupConfig{}, fmt.Errorf("load config: %s", userConfigLoadError(runtime.configPath, err))
	}
	runtime.loaded = loaded
	return startupConfig{
		runtime:     runtime,
		settings:    settings,
		settingsErr: settingsErr,
	}, nil
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

func newModel(startup startupConfig, deps modelDeps) *Model {
	config := startup.runtime
	if deps.logger == nil {
		deps.logger = noopLogger{}
	}
	if deps.serveSOCKS == nil {
		deps.serveSOCKS = proxy.ServeSOCKS
	}

	nodes := nodesForDisplay(startup.runtime.loaded.Nodes)

	model := &Model{
		config:   config,
		deps:     deps,
		state:    stateNodeList,
		nodeList: newNodeList(nodeItemsWithCurrent(nodes, "")),
		nodes:    nodes,
	}
	if startup.settingsErr != nil {
		deps.logger.Warnf("[efanadp] load settings failed path=%s err=%v", config.settingsPath, startup.settingsErr)
		model.setStatus(statusWarning, "Failed to read settings; using "+config.configPath+": "+shortError(startup.settingsErr))
	}
	if startup.settingsErr == nil && strings.TrimSpace(startup.settings.NodeName) != "" {
		model.rememberedNodeName = strings.TrimSpace(startup.settings.NodeName)
		if node, ok := model.selectNodeByName(model.rememberedNodeName); ok {
			model.setStatus(statusSuccess, "Restored previous node: "+node.Name)
			model.startupNode = &node
		} else {
			model.setStatus(statusWarning, fmt.Sprintf("Loaded %d nodes; previous node %q is not in this config", len(model.nodes), model.rememberedNodeName))
			model.rememberedNodeName = ""
		}
	}
	return model
}
