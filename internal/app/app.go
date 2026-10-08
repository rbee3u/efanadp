// Package app dispatches the command line and wires process dependencies. With
// no arguments it launches the TUI; the config-reference subcommand regenerates
// the committed config template.
package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rbee3u/efanadp/internal/tui"
)

// Run dispatches the TUI and maintenance subcommands for args.
func Run(args []string) error {
	return runArgs(args, func(configPath string) error {
		return runInteractive(configPath, tui.Run)
	}, os.Stdout)
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

func printRootHelp(w io.Writer) error {
	_, err := io.WriteString(w, `efanadp

Local SOCKS5 adapter for decrypted efanapp x365 nodes

Usage:
  efanadp [--config path]
  efanadp config-reference [--in path] [--out path]

Flags:
  -c, --config string   config file path
`)
	return err
}
