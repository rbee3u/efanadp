package tui

import (
	"fmt"
	"os"
	"path/filepath"
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
	if err := validateLogOutputPath(loaded.Log.Output, runtime.configPath, runtime.settingsPath); err != nil {
		return startupConfig{}, fmt.Errorf("load config: %w", err)
	}
	runtime.loaded = loaded
	return startupConfig{
		runtime:     runtime,
		settings:    settings,
		settingsErr: settingsErr,
	}, nil
}

// validateLogOutputPath rejects a log file that is really the config or settings
// file. Nothing else protects them: the logger opens its output for append, so a
// config path would have log lines written over private input the app only ever
// reads and cannot regenerate, and a settings path would race saveSettings,
// whose temp-file rename and the logger's appends each corrupt the other.
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

func newModel(startup startupConfig, deps modelDeps) *Model {
	runtime := startup.runtime
	if deps.logger == nil {
		deps.logger = noopLogger{}
	}
	if deps.serveSOCKS == nil {
		deps.serveSOCKS = proxy.ServeSOCKS
	}

	nodes := nodesForDisplay(startup.runtime.loaded.Nodes)

	model := &Model{
		config:   runtime,
		deps:     deps,
		state:    stateNodeList,
		nodeList: newNodeList(nodeItemsWithCurrent(nodes, "")),
		nodes:    nodes,
	}
	if startup.settingsErr != nil {
		deps.logger.Warnf("[efanadp] load settings failed path=%s err=%v", runtime.settingsPath, startup.settingsErr)
		model.setStatus(statusWarning, "Failed to read settings; using "+runtime.configPath+": "+shortError(startup.settingsErr))
		return model
	}
	if remembered := strings.TrimSpace(startup.settings.NodeName); remembered != "" {
		model.rememberedNodeName = remembered
		if node, ok := model.selectNodeByName(remembered); ok {
			model.setStatus(statusSuccess, "Restored previous node: "+node.Name)
			model.startupNode = &node
		} else {
			model.setStatus(statusWarning, fmt.Sprintf("Loaded %d nodes; previous node %q is not in this config", len(model.nodes), remembered))
			model.rememberedNodeName = ""
		}
	}
	return model
}
