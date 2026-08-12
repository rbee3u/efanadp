package config

import (
	"strings"
	"testing"
)

func TestYAMLParseErrorsDoNotExposeSourceLines(t *testing.T) {
	const marker = "private-node-marker.example"
	tests := []struct {
		name  string
		parse func() error
	}{
		{
			name: "main config",
			parse: func() error {
				_, err := parseConfigBytes([]byte("proxies: [" + marker + "\n"))
				return err
			},
		},
		{
			name: "packed template",
			parse: func() error {
				packed, err := packProxyTemplate([]byte("nodes: [" + marker + "\n"))
				if err != nil {
					return err
				}
				_, err = proxiesFromTemplate(packed, "00112233-4455-6677-8899-aabbccddeeff")
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.parse()
			if err == nil {
				t.Fatal("malformed YAML parsed successfully")
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("parse error includes source content: %q", err)
			}
			if !strings.Contains(err.Error(), "[1:") {
				t.Fatalf("parse error lost its source position: %q", err)
			}
		})
	}
}
