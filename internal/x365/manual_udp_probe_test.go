//go:build manual

package x365_test

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rbee3u/efanadp/internal/config"
	"github.com/rbee3u/efanadp/internal/x365"
)

func TestManualX365UDPProbe(t *testing.T) {
	configPath := os.Getenv("EFANADP_CONFIG")
	if configPath == "" {
		configPath = "config.yaml"
	}
	configPath = resolveManualProbeConfigPath(configPath)
	nodeIndex := 0
	if raw := os.Getenv("EFANADP_NODE_INDEX"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("EFANADP_NODE_INDEX must be numeric: %v", err)
		}
		nodeIndex = parsed
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if nodeIndex < 0 || nodeIndex >= len(loaded.Nodes) {
		t.Fatalf("EFANADP_NODE_INDEX=%d out of range; config has %d nodes", nodeIndex, len(loaded.Nodes))
	}
	node := loaded.Nodes[nodeIndex]

	const probeTimeout = 20 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()
	client, err := x365.NewClient(node, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := client.OpenUDP(ctx, "1.1.1.1", 53)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = conn.Close()
	}()

	query := manualDNSQuery("example.com")
	if _, err := conn.WriteDatagram(query); err != nil {
		t.Fatal(err)
	}

	type readResult struct {
		packet []byte
		err    error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 1500)
		n, err := conn.ReadDatagram(buf)
		resultCh <- readResult{packet: buf[:n], err: err}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		checkManualDNSResponse(t, query, result.packet)
	case <-time.After(probeTimeout):
		go func() {
			_ = conn.Close()
		}()
		t.Fatal("timed out waiting for X365 UDP DNS response")
	}
}

func resolveManualProbeConfigPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	parentPath := filepath.Join("..", path)
	if _, err := os.Stat(parentPath); err == nil {
		return parentPath
	}
	return path
}

func manualDNSQuery(domain string) []byte {
	packet := []byte{
		0x12, 0x34, // ID
		0x01, 0x00, // standard recursive query
		0x00, 0x01, // QDCOUNT
		0x00, 0x00, // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
	}
	start := 0
	for i := 0; i <= len(domain); i++ {
		if i != len(domain) && domain[i] != '.' {
			continue
		}
		label := domain[start:i]
		packet = append(packet, byte(len(label)))
		packet = append(packet, label...)
		start = i + 1
	}
	packet = append(
		packet,
		0x00,       // root label
		0x00, 0x01, // QTYPE A
		0x00, 0x01, // QCLASS IN
	)
	return packet
}

func checkManualDNSResponse(t *testing.T, query, response []byte) {
	t.Helper()
	if len(response) < 12 {
		t.Fatalf("DNS response too short: %d bytes", len(response))
	}
	if response[0] != query[0] || response[1] != query[1] {
		t.Fatalf("DNS response ID mismatch")
	}
	if response[2]&0x80 == 0 {
		t.Fatalf("DNS response QR bit is not set")
	}
	if rcode := response[3] & 0x0f; rcode != 0 {
		t.Fatalf("DNS response rcode=%d, want 0", rcode)
	}
	answers := binary.BigEndian.Uint16(response[6:8])
	t.Logf("X365 UDP DNS probe returned %d bytes with %d answers", len(response), answers)
}
