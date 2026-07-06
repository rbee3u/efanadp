package tui

import (
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
)

type viewState int

const (
	stateNodeList viewState = iota
	stateRunning
	stateQuitting
)

type statusKind int

const (
	statusInfo statusKind = iota
	statusSuccess
	statusWarning
	statusError
)

type serviceStoppedMsg struct {
	id       int
	nodeName string
	err      error
}

type connectionEventMsg struct {
	serviceID int
	event     proxy.Event
}

type runningTickMsg struct {
	serviceID int
	at        time.Time
}

type connectionItem struct {
	id        uint64
	protocol  string
	source    string
	target    string
	startedAt time.Time
	stats     *proxy.Stats
}
