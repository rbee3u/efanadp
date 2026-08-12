package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	if err := ensureDistinctConfigReferenceFiles(input, output); err != nil {
		return err
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	reference, err := config.GenerateReference(data)
	if err != nil {
		return err
	}
	return writePrivateFileAtomically(output, reference)
}

func ensureDistinctConfigReferenceFiles(input, output string) error {
	inputInfo, err := os.Stat(input)
	if err != nil {
		return err
	}
	outputInfo, err := os.Stat(output)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if os.SameFile(inputInfo, outputInfo) {
		return errors.New("config-reference output resolves to the private input file")
	}
	return nil
}

func writePrivateFileAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-reference-*.tmp")
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

func printConfigReferenceHelp(w io.Writer) error {
	_, err := fmt.Fprintf(
		w,
		"efanadp config-reference\n\nRegenerate the sanitized reference config.\n\nUsage:\n  efanadp config-reference [--in path] [--out path]\n\nFlags:\n      --in string    private config file to read (default %q)\n      --out string   reference config to write (default %q)\n",
		configReferenceInput,
		configReferenceOutput,
	)
	return err
}
