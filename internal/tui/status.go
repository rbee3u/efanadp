package tui

import (
	"strings"
	"time"
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusSuccess
	statusWarning
	statusError
)

func (m *Model) setStatus(kind statusKind, text string) {
	m.statusKind = kind
	m.status = text
	m.statusClearAt = time.Time{}
}

func (m *Model) clearStatus() {
	m.status = ""
	m.statusClearAt = time.Time{}
}

func (m *Model) setExpiringStatus(kind statusKind, text string, ttl time.Duration) {
	m.setStatus(kind, text)
	m.statusClearAt = time.Now().Add(ttl)
}

func (m *Model) clearExpiredStatus(now time.Time) {
	if !m.statusClearAt.IsZero() && !now.Before(m.statusClearAt) {
		m.clearStatus()
	}
}

func (m *Model) setModeHint(text string) {
	m.modeHint = text
	m.modeHintState = m.state
}

func (m *Model) clearModeHint() {
	m.modeHint = ""
}

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

// Preserve visible errors and warnings. clearStatus leaves statusKind intact,
// so an empty status takes precedence over its old kind.
func (m *Model) canShowSettingsWarning() bool {
	if m.status == "" {
		return true
	}
	return m.statusKind != statusError && m.statusKind != statusWarning
}
