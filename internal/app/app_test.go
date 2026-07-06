package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunArgsRunsTUIWithoutArgs(t *testing.T) {
	ran, gotConfig := runArgsForTest(t, []string{})

	if !ran {
		t.Fatal("root command did not run TUI")
	}
	if gotConfig != "" {
		t.Fatalf("config path=%q, want empty fallback", gotConfig)
	}
}

func TestRunArgsPassesConfigFlag(t *testing.T) {
	_, gotConfig := runArgsForTest(t, []string{"--config", "proxy.yaml"})

	if gotConfig != "proxy.yaml" {
		t.Fatalf("config path=%q, want proxy.yaml", gotConfig)
	}
}

func TestRunArgsPassesShortConfigFlag(t *testing.T) {
	_, gotConfig := runArgsForTest(t, []string{"-c", "proxy.yaml"})

	if gotConfig != "proxy.yaml" {
		t.Fatalf("config path=%q, want proxy.yaml", gotConfig)
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

func TestRunArgsConfigReferenceWritesReference(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "config.yaml")
	output := filepath.Join(dir, "config.reference.yaml")

	writeConfigReferenceInput(t, input)

	var stdout bytes.Buffer
	err := runArgs([]string{"config-reference", "--in", input, "--out", output}, func(string) error {
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
}

func TestRunArgsConfigReferenceAcceptsSingleDashFlags(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "config.yaml")
	output := filepath.Join(dir, "config.reference.yaml")
	writeConfigReferenceInput(t, input)

	var stdout bytes.Buffer
	err := runArgs([]string{"config-reference", "-in", input, "-out=" + output}, func(string) error {
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
	if !strings.Contains(string(data), "proxies_uuid") {
		t.Fatalf("generated reference missing proxies_uuid:\n%s", data)
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
