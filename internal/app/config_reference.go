package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rbee3u/efanadp/internal/config"
)

const (
	configReferenceCommand = "config-reference"
	configReferenceInput   = "config.yaml"
	configReferenceOutput  = "config.reference.yaml"
)

func runConfigReferenceArgs(args []string, stdout io.Writer) error {
	input := configReferenceInput
	output := configReferenceOutput
	fs := flag.NewFlagSet(configReferenceCommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&input, "in", configReferenceInput, "private config file to read")
	fs.StringVar(&output, "out", configReferenceOutput, "reference config to write")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printConfigReferenceHelp(stdout)
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("config-reference: unexpected argument %q", fs.Arg(0))
	}
	return runConfigReference(input, output)
}

func runConfigReference(input string, output string) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	reference, err := config.GenerateReference(data)
	if err != nil {
		return err
	}
	return os.WriteFile(
		output,
		reference,
		0o600,
	)
}

func printConfigReferenceHelp(w io.Writer) error {
	_, err := fmt.Fprintf(
		w,
		"efanadp config-reference\n\nRegenerate the sanitized reference config.\n\nUsage:\n  efanadp config-reference [--in path] [--out path]\n\nFlags:\n      --in string    private config file to read (default %q)\n      --out string   reference config to write (default %q)\n",
		configReferenceInput,
		configReferenceOutput,
	)
	return err
}
