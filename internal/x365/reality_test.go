package x365

import (
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestRealityFingerprintDefaultMatchesefanapp(t *testing.T) {
	for _, value := range []string{"", "chrome", "unknown"} {
		if got := realityFingerprint(value); got != utls.HelloChrome_133 {
			t.Fatalf("realityFingerprint(%q)=%+v, want %+v", value, got, utls.HelloChrome_133)
		}
	}
}

func TestBuildRealityConfig(t *testing.T) {
	node := Node{
		Host:      "example.net",
		SNI:       "example.org",
		PublicKey: testRealityPublicKey,
		ShortID:   "01020304",
	}
	got, err := buildRealityConfig(node)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerName != "example.org" {
		t.Fatalf("server name=%q, want example.org", got.ServerName)
	}
	if len(got.PublicKey) != 32 {
		t.Fatalf("public key len=%d, want 32", len(got.PublicKey))
	}
	if got.ShortID[0] != 1 || got.ShortID[3] != 4 {
		t.Fatalf("short id=%x, want decoded bytes", got.ShortID)
	}
}

func TestBuildRealityConfigFallsBackToHostForServerName(t *testing.T) {
	got, err := buildRealityConfig(Node{
		Host:      "example.net",
		PublicKey: testRealityPublicKey,
		ShortID:   "01020304",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerName != "example.net" {
		t.Fatalf("server name=%q, want host fallback", got.ServerName)
	}
}

func TestBuildRealityConfigRejectsInvalidRealityValues(t *testing.T) {
	tests := []struct {
		name   string
		node   Node
		want   string
		secret string
	}{
		{
			name: "bad public key base64",
			node: Node{
				Host:      "example.net",
				PublicKey: "private!key",
				ShortID:   "01020304",
			},
			want:   "invalid reality public key",
			secret: "private!key",
		},
		{
			name: "short public key",
			node: Node{
				Host:      "example.net",
				PublicKey: "AQID",
				ShortID:   "01020304",
			},
			want:   "reality public key must be 32 bytes",
			secret: "AQID",
		},
		{
			name: "bad short id hex",
			node: Node{
				Host:      "example.net",
				PublicKey: testRealityPublicKey,
				ShortID:   "private-sid",
			},
			want:   "invalid reality short id",
			secret: "private-sid",
		},
		{
			name: "long short id",
			node: Node{
				Host:      "example.net",
				PublicKey: testRealityPublicKey,
				ShortID:   "010203040506070809",
			},
			want:   "reality short id must be at most 8 bytes",
			secret: "010203040506070809",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildRealityConfig(tt.node)
			requireRedactedError(t, err, tt.want, tt.secret)
		})
	}
}
