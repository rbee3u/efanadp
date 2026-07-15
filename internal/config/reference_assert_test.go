package config

import (
	"strings"
	"testing"
)

func assertProxyTemplateLinesWrapped(t *testing.T, text string) {
	t.Helper()

	inTemplate := false
	lineCount := 0
	for lineNo, line := range strings.Split(text, "\n") {
		switch {
		case line == "proxies_tmpl: |":
			inTemplate = true
		case inTemplate && strings.HasPrefix(line, "  "):
			lineCount++
			payload := strings.TrimPrefix(line, "  ")
			if len(payload) > 76 {
				t.Fatalf("proxies_tmpl line %d length=%d, want <=76", lineNo+1, len(payload))
			}
		case inTemplate && strings.TrimSpace(line) != "":
			t.Fatalf("unexpected non-template content after proxies_tmpl at line %d", lineNo+1)
		}
	}
	if !inTemplate {
		t.Fatal("missing proxies_tmpl block")
	}
	if lineCount == 0 {
		t.Fatal("proxies_tmpl block is empty")
	}
}
