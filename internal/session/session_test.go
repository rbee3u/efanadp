package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/rbee3u/efanadp/internal/proxy"
	"github.com/rbee3u/efanadp/internal/sniff"
	"github.com/rbee3u/efanadp/internal/x365"
)

func testNode() x365.Node {
	return x365.Node{Name: "test-node", UUID: "00112233-4455-6677-8899-aabbccddeeff", Server: "example.org", Port: 443}
}

func TestServiceOwnsCancellationCompletionAndFinalEvents(t *testing.T) {
	started := make(chan struct{})
	cleanup := make(chan struct{})
	cancelled := make(chan struct{})
	boom := errors.New("serve failed")
	cfg := Config{Listen: "127.0.0.1:0", HandshakeTimeout: time.Second, ShadowsocksSniffConfigs: []sniff.ShadowsocksConfig{{Name: "landing", Server: "203.0.113.10", Port: 443, Method: "aes-256-gcm", Password: "<shadowsocks-password>"}}}
	start, err := prepare(testNode(), cfg, func(ctx context.Context, listen string, server proxy.Config) error {
		if listen != cfg.Listen || server.HandshakeTimeout != cfg.HandshakeTimeout || !reflect.DeepEqual(server.ShadowsocksSniffConfigs, cfg.ShadowsocksSniffConfigs) || server.Upstream == nil {
			t.Error("service configuration was not passed through")
		}
		server.OnConnection(proxy.Event{ID: 1, Type: proxy.ConnectionOpened})
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-cleanup
		server.OnConnection(proxy.Event{ID: 1, Type: proxy.ConnectionClosed})
		return boom
	})
	if err != nil {
		t.Fatal(err)
	}
	s := start()
	<-started
	if batch := s.NextEvents(); len(batch) != 1 || batch[0].Type != proxy.ConnectionOpened {
		t.Fatalf("opening batch=%v", batch)
	}
	s.Stop()
	<-cancelled
	select {
	case <-s.Done():
		t.Error("completed before server cleanup")
	default:
	}
	close(cleanup)
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("service did not complete")
	}
	if !errors.Is(s.Err(), boom) {
		t.Fatalf("completion error=%v", s.Err())
	}
	if batch := s.NextEvents(); len(batch) != 1 || batch[0].Type != proxy.ConnectionClosed {
		t.Fatalf("closing batch=%v", batch)
	}
	if batch := s.NextEvents(); batch != nil {
		t.Fatalf("events after drain=%v", batch)
	}
	s.Stop()
}

func TestPrepareRejectsInvalidNodeWithoutStartingServer(t *testing.T) {
	node := testNode()
	node.UUID = "invalid"
	start, err := prepare(node, Config{}, func(context.Context, string, proxy.Config) error { t.Error("unexpected server start"); return nil })
	if err == nil || start != nil {
		t.Fatal("invalid node was prepared")
	}
}
