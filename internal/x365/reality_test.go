package x365

import (
	"bytes"
	"strings"
	"testing"

	utls "github.com/refraction-networking/utls"
)

// The fingerprint decides which browser's ClientHello the REALITY transport
// imitates, so a wrong mapping makes the handshake look like a client the node
// did not advertise. An unrecognized value falls back to Chrome to match efanapp.
func TestRealityFingerprintMapsNodeValues(t *testing.T) {
	tests := []struct {
		value string
		want  utls.ClientHelloID
	}{
		{value: "firefox", want: utls.HelloFirefox_Auto},
		{value: "safari", want: utls.HelloSafari_Auto},
		{value: "ios", want: utls.HelloIOS_Auto},
		{value: "edge", want: utls.HelloEdge_Auto},
		{value: "random", want: utls.HelloRandomizedALPN},
		{value: "randomized", want: utls.HelloRandomizedALPN},
		{value: "", want: utls.HelloChrome_133},
		{value: "chrome", want: utls.HelloChrome_133},
		{value: "unknown", want: utls.HelloChrome_133},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := realityFingerprint(tt.value); got != tt.want {
				t.Fatalf("realityFingerprint(%q)=%+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

// The node carries the public key as base64 and the short ID as hex, but the
// handshake needs raw bytes: the key goes into the X25519 exchange and the short
// ID is copied into the ClientHello session ID, so both must arrive decoded.
func TestBuildRealityConfigDecodesKeyMaterial(t *testing.T) {
	got, err := buildRealityConfig(Node{
		Host:      "example.net",
		SNI:       "example.org",
		PublicKey: testRealityPublicKey,
		ShortID:   "01020304",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PublicKey) != realityPublicKeySize {
		t.Fatalf("public key len=%d, want %d", len(got.PublicKey), realityPublicKeySize)
	}
	if want := []byte{1, 2, 3, 4}; !bytes.Equal(got.ShortID, want) {
		t.Fatalf("short id=%x, want decoded %x", got.ShortID, want)
	}
}

// The server name becomes the TLS SNI, which is the whole disguise: dialing with
// the wrong one, or with an empty one, makes the connection stand out from the
// browser traffic the transport imitates. SNI wins when set, host is the
// fallback, and a node with neither must fail here rather than dial.
func TestBuildRealityConfigResolvesServerName(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		sni     string
		want    string
		wantErr string
	}{
		{name: "sni wins over host", host: "example.net", sni: "example.org", want: "example.org"},
		{name: "host fallback", host: "example.net", want: "example.net"},
		{name: "sni only", sni: "example.org", want: "example.org"},
		{name: "neither is rejected", wantErr: "reality server name is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildRealityConfig(Node{
				Host:      tt.host,
				SNI:       tt.sni,
				PublicKey: testRealityPublicKey,
				ShortID:   "01020304",
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err=%v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ServerName != tt.want {
				t.Fatalf("server name=%q, want %q", got.ServerName, tt.want)
			}
		})
	}
}

// A node reaches the dial having already been validated by ParseNode, so this
// only has to prove buildRealityConfig re-validates instead of trusting it: the
// full condition matrix for both validators lives at that parse boundary, in
// TestParseNodeRejectsInvalidRealityMaterialWithoutLeakingNodeMaterial.
func TestBuildRealityConfigRevalidatesRealityMaterial(t *testing.T) {
	tests := []struct {
		name   string
		node   Node
		want   string
		secret string
	}{
		{
			name:   "public key",
			node:   Node{Host: "example.net", PublicKey: "private!key", ShortID: "01020304"},
			want:   "invalid reality public key",
			secret: "private!key",
		},
		{
			name:   "short id",
			node:   Node{Host: "example.net", PublicKey: testRealityPublicKey, ShortID: "private-sid"},
			want:   "invalid reality short id",
			secret: "private-sid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRealityConfig(tt.node)
			requireRedactedError(t, err, tt.want, tt.secret)
		})
	}
}
