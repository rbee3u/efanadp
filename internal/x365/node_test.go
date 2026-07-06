package x365

import (
	"strings"
	"testing"
)

const testRealityPublicKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"

func requireRedactedError(t *testing.T, err error, want string, secrets ...string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err=%v, want %q", err, want)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("err=%q leaks %q", err, secret)
		}
	}
}

func TestParseNodeKeepsPrivateFingerprint(t *testing.T) {
	node, err := ParseNode(
		"test",
		"x365://00112233-4455-6677-8899-aabbccddeeff@example.com:443?"+
			"path=/x&host=example.net&sni=example.net&pbk="+testRealityPublicKey+"&sid=01020304&fp=safari",
	)
	if err != nil {
		t.Fatal(err)
	}
	if node.realityFingerprint != "safari" {
		t.Fatalf("realityFingerprint=%q, want safari", node.realityFingerprint)
	}
}

func TestParseNodeRejectsInvalidUserID(t *testing.T) {
	_, err := ParseNode(
		"test",
		"x365://not-a-uuid@example.com:443?"+
			"path=/x&host=example.net&sni=example.net&pbk=test-public-key&sid=01020304",
	)
	if err == nil || !strings.Contains(err.Error(), "invalid user id") {
		t.Fatalf("err=%v, want invalid user id", err)
	}
}

func TestParseNodeRejectsInvalidUserIDWithoutLeakingNodeMaterial(t *testing.T) {
	_, err := ParseNode(
		"test",
		"x365://not-a-uuid@private.example:443?"+
			"path=/hidden&host=origin.example&sni=origin.example&pbk=private-key&sid=01020304",
	)
	requireRedactedError(t, err, "invalid user id", "private.example", "/hidden", "origin.example", "private-key", "01020304")
}

func TestParseNodeRejectsMalformedURLWithoutLeakingNodeMaterial(t *testing.T) {
	_, err := ParseNode(
		"test",
		"x365://00112233-4455-6677-8899-aabbccddeeff@secret%zz.example.org:443?"+
			"path=/hidden-node&host=origin.example.net&sni=origin.example.net&pbk=test-public-key&sid=01020304",
	)
	requireRedactedError(
		t, err, "invalid x365 URL",
		"00112233-4455-6677-8899-aabbccddeeff",
		"secret%zz.example.org",
		"/hidden-node",
		"origin.example.net",
		"test-public-key",
		"01020304",
	)
}

func TestParseNodeRejectsInvalidPort(t *testing.T) {
	for _, port := range []string{"0", "65536"} {
		_, err := ParseNode(
			"test",
			"x365://00112233-4455-6677-8899-aabbccddeeff@example.com:"+port+"?"+
				"path=/x&host=example.net&sni=example.net&pbk="+testRealityPublicKey+"&sid=01020304",
		)
		if err == nil || !strings.Contains(err.Error(), "invalid port") {
			t.Fatalf("port %s err=%v, want invalid port", port, err)
		}
	}
}

func TestParseNodeRejectsInvalidPublicKeyWithoutLeakingNodeMaterial(t *testing.T) {
	tests := []struct {
		name string
		pbk  string
		want string
	}{
		{
			name: "bad base64",
			pbk:  "private!key",
			want: "invalid reality public key",
		},
		{
			name: "wrong length",
			pbk:  "AQID",
			want: "reality public key must be 32 bytes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNode(
				"test",
				"x365://00112233-4455-6677-8899-aabbccddeeff@private.example:443?"+
					"path=/hidden&host=origin.example&sni=origin.example&pbk="+tt.pbk+"&sid=01020304",
			)
			requireRedactedError(t, err, tt.want, "private.example", "/hidden", "origin.example", tt.pbk, "01020304")
		})
	}
}

func TestParseNodeRejectsInvalidShortIDWithoutLeakingNodeMaterial(t *testing.T) {
	tests := []struct {
		name string
		sid  string
		want string
	}{
		{
			name: "bad hex",
			sid:  "private-sid",
			want: "invalid reality short id",
		},
		{
			name: "too long",
			sid:  "010203040506070809",
			want: "reality short id must be at most 8 bytes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNode(
				"test",
				"x365://00112233-4455-6677-8899-aabbccddeeff@private.example:443?"+
					"path=/hidden&host=origin.example&sni=origin.example&pbk="+testRealityPublicKey+"&sid="+tt.sid,
			)
			requireRedactedError(t, err, tt.want, "private.example", "/hidden", "origin.example", tt.sid)
		})
	}
}

func TestNodeUserID(t *testing.T) {
	node := Node{UUID: "00112233-4455-6677-8899-aabbccddeeff"}
	userID, err := node.UserID()
	if err != nil {
		t.Fatal(err)
	}
	if userID[0] != 0x00 || userID[15] != 0xff {
		t.Fatalf("user id=%x, want decoded UUID bytes", userID)
	}
}
