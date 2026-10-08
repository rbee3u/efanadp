package config

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/rbee3u/efanadp/internal/x365"
)

// Bound decompressed input before parsing to limit expansion from small
// payloads.
const maxProxyTemplateBytes = 4 << 20

var errProxyTemplateTooLarge = fmt.Errorf("proxies_tmpl exceeds %d-byte unpacked limit", maxProxyTemplateBytes)

func proxyGroupsFromConfig(proxies yaml.MapSlice, encodedTemplate, uuid string) (yaml.MapSlice, error) {
	if len(proxies) > 0 {
		if strings.TrimSpace(encodedTemplate) != "" {
			return nil, errors.New("proxies and proxies_tmpl cannot both be set")
		}
		return proxies, nil
	}
	return proxiesFromTemplate(encodedTemplate, uuid)
}

func proxiesFromTemplate(encoded, uuid string) (yaml.MapSlice, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil, configError(MissingProxies, errors.New("missing or empty proxies field"))
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, configError(MissingUUID, errors.New("missing proxies_uuid for proxies_tmpl"))
	}
	if _, err := x365.ParseUserID(uuid); err != nil {
		kind := InvalidUUID
		if uuid == "<your-x365-uuid>" {
			kind = UUIDPlaceholder
		}
		return nil, configError(kind, fmt.Errorf("invalid proxies_uuid: %w", err))
	}

	decoded, err := unpackProxyTemplate(encoded)
	if err != nil {
		return nil, err
	}
	var tmpl yaml.MapSlice
	if err := yaml.Unmarshal(decoded, &tmpl); err != nil {
		return nil, configError(TemplateYAML, fmt.Errorf("parse proxies_tmpl YAML: %s", safeYAMLError(err)))
	}
	if len(tmpl) == 0 {
		return nil, configError(TemplateEmpty, errors.New("proxies_tmpl contains no proxy groups"))
	}
	return mapProxyGroups(tmpl, func(value any) ([]string, error) {
		return fillProxyTemplateURLs(value, uuid)
	})
}

func packProxyTemplate(data []byte) (string, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			return "", fmt.Errorf("compress proxies_tmpl: write failed: %w; close failed: %w", err, closeErr)
		}
		return "", fmt.Errorf("compress proxies_tmpl: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("compress proxies_tmpl: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// Accept legacy uncompressed base64 YAML within the same size limit.
func unpackProxyTemplate(encoded string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, configError(TemplateBase64, fmt.Errorf("decode proxies_tmpl: %w", err))
	}
	unpacked, err := unzipProxyTemplate(decoded)
	if err == nil {
		return unpacked, nil
	}
	if errors.Is(err, errProxyTemplateTooLarge) || len(decoded) > maxProxyTemplateBytes {
		return nil, configError(TemplateTooLarge, fmt.Errorf("decompress proxies_tmpl: %w", errProxyTemplateTooLarge))
	}
	if isLegacyProxyTemplateYAML(decoded) {
		return decoded, nil
	}
	return nil, configError(TemplateCompression, fmt.Errorf("decompress proxies_tmpl: %w", err))
}

func unzipProxyTemplate(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(zr, maxProxyTemplateBytes+1))
	closeErr := zr.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maxProxyTemplateBytes {
		return nil, errProxyTemplateTooLarge
	}
	return data, nil
}

func isLegacyProxyTemplateYAML(data []byte) bool {
	var tmpl yaml.MapSlice
	return yaml.Unmarshal(data, &tmpl) == nil && len(tmpl) > 0
}

func mapProxyGroups(
	source yaml.MapSlice,
	normalizeURLs func(any) ([]string, error),
) (yaml.MapSlice, error) {
	groups := make(yaml.MapSlice, 0, len(source))
	for _, item := range source {
		urls, err := normalizeURLs(item.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fmt.Sprint(item.Key), err)
		}
		groups = append(groups, yaml.MapItem{Key: item.Key, Value: urls})
	}
	return groups, nil
}

func parseProxyURLs(value any) ([]string, error) {
	return collectProxyURLs(value, ProxyListShape, func(raw string) (string, error) {
		if !strings.HasPrefix(raw, "x365://") {
			return "", configError(UnsupportedURL, errors.New("unsupported proxy URL"))
		}
		return raw, nil
	})
}

func fillProxyTemplateURLs(value any, uuid string) ([]string, error) {
	return collectProxyURLs(value, TemplateListShape, func(raw string) (string, error) {
		return fillProxyTemplateURL(raw, uuid)
	})
}

func collectProxyURLs(
	value any,
	listKind ErrorKind,
	normalize func(string) (string, error),
) ([]string, error) {
	itemKind := "x365 URLs"
	emptyMessage := "empty proxy URL list"
	emptyKind := EmptyProxyList
	if listKind == TemplateListShape {
		itemKind = "x365 URL templates"
		emptyMessage = "empty proxy URL template list"
		emptyKind = EmptyTemplateList
	}

	items, ok := proxyURLStrings(value)
	if !ok {
		return nil, configError(listKind, fmt.Errorf("expected a list of %s, got %T", itemKind, value))
	}
	urls := make([]string, 0, len(items))
	for _, item := range items {
		raw := strings.TrimSpace(item)
		if raw == "" {
			continue
		}
		url, err := normalize(raw)
		if err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return nil, configError(emptyKind, errors.New(emptyMessage))
	}
	return urls, nil
}

func proxyURLStrings(value any) ([]string, bool) {
	switch items := value.(type) {
	case []string:
		return items, true
	case []any:
		urls := make([]string, 0, len(items))
		for _, item := range items {
			raw, ok := item.(string)
			if !ok {
				return nil, false
			}
			urls = append(urls, raw)
		}
		return urls, true
	default:
		return nil, false
	}
}

func fillProxyTemplateURL(raw, uuid string) (string, error) {
	if !strings.HasPrefix(raw, "x365://") {
		return "", configError(UnsupportedTemplateURL, errors.New("unsupported proxy URL template"))
	}
	const emptyUserPrefix = "x365://@"
	if strings.HasPrefix(raw, emptyUserPrefix) {
		return "x365://" + uuid + raw[len("x365://"):], nil
	}
	return "", configError(TemplateUserInfo, errors.New("x365 URL template must omit UUID as x365://@server"))
}
