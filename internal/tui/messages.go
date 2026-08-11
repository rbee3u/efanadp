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

// connectionEventMsg carries every event that was queued when the waiter woke.
// Draining the queue in one Update keeps the producer's buffer from filling
// during a connection burst: each Update triggers a full re-render, so applying
// one event per render made the render cost the drain rate.
type connectionEventMsg struct {
	serviceID int
	events    []proxy.Event
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
