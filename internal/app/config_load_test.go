package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rbee3u/efanadp/internal/config"
)

func TestUserConfigLoadErrorMapsCommonConfigFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "uuid placeholder",
			err:  &config.Error{Kind: config.UUIDPlaceholder, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "proxies_uuid is still the reference placeholder",
		},
		{
			name: "template base64",
			err:  &config.Error{Kind: config.TemplateBase64, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "proxies_tmpl is not valid base64 template data",
		},
		{
			name: "template compression",
			err:  &config.Error{Kind: config.TemplateCompression, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "proxies_tmpl is not valid zlib-compressed template data",
		},
		{
			name: "template too large",
			err:  &config.Error{Kind: config.TemplateTooLarge, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "proxies_tmpl is too large",
		},
		{
			name: "template yaml",
			err:  &config.Error{Kind: config.TemplateYAML, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "proxies_tmpl does not unpack to valid YAML",
		},
		{
			name: "empty template groups",
			err:  &config.Error{Kind: config.TemplateEmpty, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "proxies_tmpl contains no proxy groups",
		},
		{
			name: "template includes uuid",
			err:  &config.Error{Kind: config.TemplateUserInfo, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "x365 templates in proxies_tmpl must be x365://@server...",
		},
		{
			name: "proxy list shape",
			err:  &config.Error{Kind: config.ProxyListShape, Err: errors.New("config.yaml: diagnostic wording can change")},
			want: "Proxy groups in proxies have the wrong format",
		},
		{
			name: "strip path prefix",
			err:  errors.New("config.yaml: unexpected low-level error"),
			want: "unexpected low-level error",
		},
		{
			name: "missing file",
			err:  fmt.Errorf("open config.yaml: %w", os.ErrNotExist),
			want: "Config file config.yaml was not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userConfigLoadError("config.yaml", tt.err)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("userConfigLoadError()=%q, want to contain %q", got, tt.want)
			}
			if strings.HasPrefix(got, "config.yaml: ") {
				t.Fatalf("userConfigLoadError()=%q, want path prefix stripped", got)
			}
		})
	}
}
