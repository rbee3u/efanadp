package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	for _, rule := range configLoadErrorRules {
		if strings.Contains(raw, rule.match) {
			return fmt.Sprintf(rule.message, detail)
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

type configLoadErrorRule struct {
	match   string
	message string
}

var configLoadErrorRules = []configLoadErrorRule{
	{
		match:   "<your-x365-uuid>",
		message: "proxies_uuid is still the reference placeholder; replace it with your account UUID (%s)",
	},
	{
		match:   "invalid proxies_uuid",
		message: "proxies_uuid is invalid; confirm the placeholder from config.reference.yaml was replaced (%s)",
	},
	{
		match:   "missing proxies_uuid",
		message: "proxies_uuid is missing; fill in the account UUID in config.yaml (%s)",
	},
	{
		match:   "missing or empty proxies field",
		message: "Config has no usable proxies; check that the full config or template was copied (%s)",
	},
	{
		match:   "decode proxies_tmpl",
		message: "proxies_tmpl is not valid base64 template data; regenerate config.reference.yaml (%s)",
	},
	{
		match:   "decompress proxies_tmpl",
		message: "proxies_tmpl is not valid zlib-compressed template data; regenerate config.reference.yaml (%s)",
	},
	{
		match:   "parse proxies_tmpl YAML",
		message: "proxies_tmpl does not unpack to valid YAML; regenerate config.reference.yaml (%s)",
	},
	{
		match:   "proxies_tmpl contains no proxy groups",
		message: "proxies_tmpl contains no proxy groups; confirm the reference config was generated completely (%s)",
	},
	{
		match:   "x365 URL template must omit UUID",
		message: "x365 templates in proxies_tmpl must be x365://@server... without a UUID (%s)",
	},
	{
		match:   "unsupported proxy URL template",
		message: "proxies_tmpl contains a non-x365 template; keep only x365:// nodes (%s)",
	},
	{
		match:   "unsupported proxy URL",
		message: "Config contains an unsupported proxy URL; keep only x365:// nodes (%s)",
	},
	{
		match:   "expected a list of x365 URL templates",
		message: "Proxy groups in proxies_tmpl have the wrong format; expected lists of x365 URLs (%s)",
	},
	{
		match:   "expected a list of x365 URLs",
		message: "Proxy groups in proxies have the wrong format; expected lists of x365 URLs (%s)",
	},
	{
		match:   "empty proxy URL template list",
		message: "A proxy group in proxies_tmpl is empty; regenerate the reference config (%s)",
	},
	{
		match:   "empty proxy URL list",
		message: "A proxy group in proxies is empty; check the config content (%s)",
	},
	{
		match:   "parse YAML",
		message: "Failed to parse YAML; check indentation and field formats (%s)",
	},
	{
		match:   "contains no x365 nodes",
		message: "Config contains no x365 nodes; confirm proxies includes x365:// links (%s)",
	},
}
