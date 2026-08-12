package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The root command launches the TUI and hands it a config path. Every accepted
// spelling has to reach the same place: a flag form that silently parses as
// something else would start the TUI against the wrong config.
func TestRunArgsRunsTUIWithConfigPath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no args uses empty fallback", args: []string{}, want: ""},
		{name: "long flag", args: []string{"--config", "proxy.yaml"}, want: "proxy.yaml"},
		{name: "short flag", args: []string{"-c", "proxy.yaml"}, want: "proxy.yaml"},
		{name: "long flag with equals", args: []string{"--config=proxy.yaml"}, want: "proxy.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran, gotConfig := runArgsForTest(t, tt.args)

			if !ran {
				t.Fatal("root command did not run TUI")
			}
			if gotConfig != tt.want {
				t.Fatalf("config path=%q, want %q", gotConfig, tt.want)
			}
		})
	}
}

func TestRunArgsRejectsUnknownArg(t *testing.T) {
	ran := false
	var stdout bytes.Buffer
	err := runArgs([]string{"unknown"}, func(string) error {
		ran = true
		return nil
	}, &stdout)
	if err == nil || !strings.Contains(err.Error(), `unexpected argument "unknown"`) {
		t.Fatalf("error=%v, want unknown argument", err)
	}
	if ran {
		t.Fatal("TUI ran for unknown argument")
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q, want empty", stdout.String())
	}
}

func TestRunArgsWritesRootHelp(t *testing.T) {
	ran := false
	var stdout bytes.Buffer
	err := runArgs([]string{"--help"}, func(string) error {
		ran = true
		return nil
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("TUI ran for help")
	}
	if got := stdout.String(); !strings.Contains(got, "Usage:") || !strings.Contains(got, "--config") {
		t.Fatalf("help output=%q, want root usage", got)
	}
}

// The generated template is the file that gets committed, so every accepted flag
// spelling must produce the same sanitized output. A flag form that parsed as
// something else would write the reference somewhere unchecked, or write a
// reference that still carries the account UUID.
func TestRunArgsConfigReferenceWritesSanitizedReference(t *testing.T) {
	flagForms := map[string]func(input, output string) []string{
		"double dash": func(input, output string) []string {
			return []string{"config-reference", "--in", input, "--out", output}
		},
		"single dash": func(input, output string) []string {
			return []string{"config-reference", "-in", input, "-out=" + output}
		},
	}

	for name, buildArgs := range flagForms {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "config.yaml")
			output := filepath.Join(dir, "config.reference.yaml")
			writeConfigReferenceInput(t, input)

			var stdout bytes.Buffer
			err := runArgs(buildArgs(input, output), func(string) error {
				t.Fatal("TUI should not run for config-reference")
				return nil
			}, &stdout)
			if err != nil {
				t.Fatal(err)
			}

			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, "00112233-4455-6677-8899-aabbccddeeff") {
				t.Fatalf("generated reference leaks UUID:\n%s", text)
			}
			if !strings.Contains(text, "proxies_uuid") {
				t.Fatalf("generated reference missing proxies_uuid:\n%s", text)
			}
		})
	}
}

func TestRunConfigReferenceAtomicallyReplacesOutputWithPrivateMode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "config.yaml")
	output := filepath.Join(dir, "config.reference.yaml")
	writeConfigReferenceInput(t, input)
	//nolint:gosec // The broad mode is the pre-existing condition under test.
	if err := os.WriteFile(output, []byte("old partial data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runConfigReference(input, output); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("output mode=%o, want 600", got)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "old partial data") || !strings.Contains(string(data), "proxies_uuid") {
		t.Fatalf("output was not replaced with the complete reference: %q", data)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".config-reference-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary reference files left behind: %v", temps)
	}
}

func TestRunConfigReferenceRefusesToReplacePrivateInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "config.yaml")
	writeConfigReferenceInput(t, input)
	want, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}

	aliases := []struct {
		name   string
		output string
	}{
		{name: "same path", output: input},
	}
	link := filepath.Join(dir, "private-config-link.yaml")
	if err := os.Symlink(input, link); err == nil {
		aliases = append(aliases, struct {
			name   string
			output string
		}{name: "symlink alias", output: link})
	}

	for _, tt := range aliases {
		t.Run(tt.name, func(t *testing.T) {
			err := runConfigReference(input, tt.output)
			if err == nil || !strings.Contains(err.Error(), "private input file") {
				t.Fatalf("err=%v, want private input collision", err)
			}
			got, readErr := os.ReadFile(input)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("private input changed after rejected config-reference output")
			}
			temps, globErr := filepath.Glob(filepath.Join(dir, ".config-reference-*.tmp"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(temps) != 0 {
				t.Fatalf("temporary reference files left behind: %v", temps)
			}
		})
	}
}

func writeConfigReferenceInput(t *testing.T, path string) {
	t.Helper()

	err := os.WriteFile(path, []byte(`
listen: 127.0.0.1:10866
proxies:
  test:
    - x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?path=/x&host=example.net&sni=example.net&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=01020304
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func runArgsForTest(t *testing.T, args []string) (bool, string) {
	t.Helper()

	ran := false
	gotConfig := ""
	var stdout bytes.Buffer
	err := runArgs(args, func(configPath string) error {
		ran = true
		gotConfig = configPath
		return nil
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q, want empty", stdout.String())
	}
	return ran, gotConfig
}
