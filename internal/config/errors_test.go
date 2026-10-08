package config

import (
	"errors"
	"fmt"
	"testing"
)

func TestConfigFailuresCarryCategoriesThroughWrapping(t *testing.T) {
	tests := []struct {
		name string
		kind ErrorKind
		fail func() error
	}{
		{"missing proxies", MissingProxies, func() error { _, err := parseConfigBytes(nil); return err }},
		{"invalid yaml", InvalidYAML, func() error { _, err := parseConfigBytes([]byte("proxies: [")); return err }},
		{"missing uuid", MissingUUID, func() error { _, err := proxiesFromTemplate("placeholder", ""); return err }},
		{"placeholder uuid", UUIDPlaceholder, func() error { _, err := proxiesFromTemplate("placeholder", "<your-x365-uuid>"); return err }},
		{"invalid uuid", InvalidUUID, func() error { _, err := proxiesFromTemplate("placeholder", "invalid"); return err }},
		{"base64", TemplateBase64, func() error { _, err := unpackProxyTemplate("!"); return err }},
		{"compression", TemplateCompression, func() error { _, err := unpackProxyTemplate("aGVsbG8="); return err }},
		{"proxy shape", ProxyListShape, func() error { _, err := parseProxyURLs(42); return err }},
		{"empty proxies", EmptyProxyList, func() error { _, err := parseProxyURLs([]string{}); return err }},
		{"unsupported proxy", UnsupportedURL, func() error { _, err := parseProxyURLs([]string{"https://example.org"}); return err }},
		{"template shape", TemplateListShape, func() error { _, err := fillProxyTemplateURLs(42, "unused"); return err }},
		{"empty template", EmptyTemplateList, func() error { _, err := fillProxyTemplateURLs([]string{}, "unused"); return err }},
		{"unsupported template", UnsupportedTemplateURL, func() error { _, err := fillProxyTemplateURL("https://example.org", "unused"); return err }},
		{"template userinfo", TemplateUserInfo, func() error { _, err := fillProxyTemplateURL("x365://placeholder@example.org", "unused"); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := tt.fail()
			if cause == nil {
				t.Fatal("expected error")
			}
			wrapped := fmt.Errorf("example.yaml: %w", cause)
			var classified *Error
			if !errors.As(wrapped, &classified) || classified.Kind != tt.kind {
				t.Fatalf("kind=%v want=%v", classified, tt.kind)
			}
			if !errors.Is(wrapped, cause) {
				t.Fatal("error chain was lost")
			}
		})
	}
}
