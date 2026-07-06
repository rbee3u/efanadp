package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestUserConfigLoadErrorMapsCommonConfigFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "uuid placeholder",
			err:  errors.New(`config.yaml: invalid proxies_uuid: parse UUID "<your-x365-uuid>": invalid UUID length`),
			want: "proxies_uuid is still the reference placeholder",
		},
		{
			name: "template base64",
			err:  errors.New("config.yaml: decode proxies_tmpl: illegal base64 data at input byte 0"),
			want: "proxies_tmpl is not valid base64 template data",
		},
		{
			name: "template compression",
			err:  errors.New("config.yaml: decompress proxies_tmpl: zlib: invalid header"),
			want: "proxies_tmpl is not valid zlib-compressed template data",
		},
		{
			name: "template yaml",
			err:  errors.New("config.yaml: parse proxies_tmpl YAML: did not find expected key"),
			want: "proxies_tmpl does not unpack to valid YAML",
		},
		{
			name: "empty template groups",
			err:  errors.New("config.yaml: proxies_tmpl contains no proxy groups"),
			want: "proxies_tmpl contains no proxy groups",
		},
		{
			name: "template includes uuid",
			err:  errors.New("config.yaml: test: x365 URL template must omit UUID as x365://@server"),
			want: "x365 templates in proxies_tmpl must be x365://@server...",
		},
		{
			name: "proxy list shape",
			err:  errors.New("config.yaml: test: expected a list of x365 URLs, got map[string]interface {}"),
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
