// Package session owns one local SOCKS service run independently of its UI.
package session

import (
	"context"
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
	"github.com/rbee3u/efanadp/internal/sniff"
	"github.com/rbee3u/efanadp/internal/x365"
)

// Service is a running service and its connection-event source. Stop requests
// cancellation without waiting; Done closes after the server and its handlers
// finish. Err is read only after Done. NextEvents has one consumer and drains
// queued events before returning nil at completion.
type Service interface {
	Stop()
	Done() <-chan struct{}
	Err() error
	NextEvents() []proxy.Event
}

// StartFunc starts a service validated offline by Prepare.
type StartFunc func() Service

// Config supplies one immutable service configuration and concurrent logger.
type Config struct {
	Listen                  string
	Timeout                 time.Duration
	HandshakeTimeout        time.Duration
	ShadowsocksSniffConfigs []sniff.ShadowsocksConfig
	Logger                  proxy.Logger
}

// Prepare validates node authentication without opening sockets. Its result
// starts an independent service using the supplied configuration snapshot.
func Prepare(node x365.Node, cfg Config) (StartFunc, error) {
	return prepare(node, cfg, proxy.ServeSOCKS)
}

func prepare(node x365.Node, cfg Config, serve func(context.Context, string, proxy.Config) error) (StartFunc, error) {
	upstream, err := x365.NewClient(node, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	return func() Service {
		ctx, cancel := context.WithCancel(context.Background())
		s := &service{
			cancel: cancel,
			done:   make(chan struct{}),
			events: newConnectionQueue(),
		}
		serverConfig := proxy.Config{
			Upstream:                upstream,
			HandshakeTimeout:        cfg.HandshakeTimeout,
			ShadowsocksSniffConfigs: cfg.ShadowsocksSniffConfigs,
			Logger:                  cfg.Logger,
			OnConnection:            s.events.push,
		}
		go func() {
			defer cancel()
			s.err = serve(ctx, cfg.Listen, serverConfig)
			close(s.done)
		}()
		return s
	}, nil
}

type service struct {
	cancel context.CancelFunc
	done   chan struct{}
	events *connectionQueue
	err    error
}

func (s *service) Stop() {
	s.cancel()
}

func (s *service) Done() <-chan struct{} {
	return s.done
}

func (s *service) Err() error {
	return s.err
}

func (s *service) NextEvents() []proxy.Event {
	for {
		if batch := s.events.take(); batch != nil {
			return batch
		}
		select {
		case <-s.events.signal:
		case <-s.done:
			return s.events.take()
		}
	}
}
