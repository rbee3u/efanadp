package tui

import "strings"

// shortError truncates an error message to a displayable length for status lines.
func shortError(err error) string {
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "unknown error"
	}
	const maxRunes = 96
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}
