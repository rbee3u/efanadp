package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/rbee3u/efanadp/internal/config"
)

func userConfigLoadError(path string, err error) string {
	raw := strings.TrimSpace(err.Error())
	detail := configLoadErrorDetail(path, raw)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Sprintf(
			"Config file %s was not found; check the path. "+
				"For first use, copy config.reference.yaml to config.yaml and fill in proxies_uuid",
			path,
		)
	}
	var failure *config.Error
	if errors.As(err, &failure) {
		if message, ok := configLoadMessages[failure.Kind]; ok {
			return fmt.Sprintf(message, detail)
		}
	}
	return detail
}

func configLoadErrorDetail(path, raw string) string {
	raw = strings.TrimSpace(raw)
	prefix := strings.TrimSpace(path) + ": "
	if trimmed, ok := strings.CutPrefix(raw, prefix); ok {
		return strings.TrimSpace(trimmed)
	}
	return raw
}

var configLoadMessages = map[config.ErrorKind]string{
	config.UUIDPlaceholder:        "proxies_uuid is still the reference placeholder; replace it with your account UUID (%s)",
	config.InvalidUUID:            "proxies_uuid is invalid; confirm the placeholder from config.reference.yaml was replaced (%s)",
	config.MissingUUID:            "proxies_uuid is missing; fill in the account UUID in config.yaml (%s)",
	config.MissingProxies:         "Config has no usable proxies; check that the full config or template was copied (%s)",
	config.TemplateBase64:         "proxies_tmpl is not valid base64 template data; regenerate config.reference.yaml (%s)",
	config.TemplateTooLarge:       "proxies_tmpl is too large; regenerate config.reference.yaml from a normal node set (%s)",
	config.TemplateCompression:    "proxies_tmpl is not valid zlib-compressed template data; regenerate config.reference.yaml (%s)",
	config.TemplateYAML:           "proxies_tmpl does not unpack to valid YAML; regenerate config.reference.yaml (%s)",
	config.TemplateEmpty:          "proxies_tmpl contains no proxy groups; confirm the reference config was generated completely (%s)",
	config.TemplateUserInfo:       "x365 templates in proxies_tmpl must be x365://@server... without a UUID (%s)",
	config.UnsupportedTemplateURL: "proxies_tmpl contains a non-x365 template; keep only x365:// nodes (%s)",
	config.UnsupportedURL:         "Config contains an unsupported proxy URL; keep only x365:// nodes (%s)",
	config.TemplateListShape:      "Proxy groups in proxies_tmpl have the wrong format; expected lists of x365 URLs (%s)",
	config.ProxyListShape:         "Proxy groups in proxies have the wrong format; expected lists of x365 URLs (%s)",
	config.EmptyTemplateList:      "A proxy group in proxies_tmpl is empty; regenerate the reference config (%s)",
	config.EmptyProxyList:         "A proxy group in proxies is empty; check the config content (%s)",
	config.InvalidYAML:            "Failed to parse YAML; check indentation and field formats (%s)",
	config.NoNodes:                "Config contains no x365 nodes; confirm proxies includes x365:// links (%s)",
}
