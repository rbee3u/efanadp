package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rbee3u/efanadp/internal/tui"
)

const appShort = "Local SOCKS5 adapter for decrypted efanapp x365 nodes"

func Run(args []string) error {
	return runArgs(args, runTUI, os.Stdout)
}

func runArgs(args []string, runTUI func(configPath string) error, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case configReferenceCommand:
			return runConfigReferenceArgs(args[1:], stdout)
		case "help":
			return runHelp(args[1:], stdout)
		}
	}
	return runRootArgs(args, runTUI, stdout)
}

func runRootArgs(args []string, runTUI func(configPath string) error, stdout io.Writer) error {
	configPath := ""
	fs := flag.NewFlagSet("efanadp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configPath, "config", "", "config file path")
	fs.StringVar(&configPath, "c", "", "config file path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printRootHelp(stdout)
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return runTUI(configPath)
}

func runHelp(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return printRootHelp(stdout)
	}
	if len(args) == 1 && args[0] == configReferenceCommand {
		return printConfigReferenceHelp(stdout)
	}
	return fmt.Errorf("unknown help topic %q", strings.Join(args, " "))
}

func runTUI(configPath string) error {
	return tui.Run(tui.Options{
		ConfigPath:       configPath,
		SettingsPath:     tui.DefaultSettingsPath(),
		Timeout:          10 * time.Second,
		HandshakeTimeout: 15 * time.Second,
	})
}

func printRootHelp(w io.Writer) error {
	_, err := fmt.Fprintf(w, "efanadp\n\n%s\n\nUsage:\n  efanadp [--config path]\n  efanadp config-reference [--in path] [--out path]\n\nFlags:\n  -c, --config string   config file path\n", appShort)
	return err
}
