package x365

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Golden bytes anchor the recovered wire format; a round trip could hide
// matching encoder and decoder errors.
func TestBuildX365Request(t *testing.T) {
	const (
		header = "58333635" + "01"
		user   = "00112233445566778899aabbccddeeff"
	)

	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		host    string
		port    int
		command x365Command
		want    string
	}{
		{
			name:    "tcp direct destination",
			host:    "example.com",
			port:    443,
			command: x365CommandTCP,
			want:    header + "01" + user + "01bb" + "02" + "0b" + "6578616d706c652e636f6d",
		},
		{
			name:    "udp domain",
			host:    "example.com",
			port:    53,
			command: x365CommandUDP,
			want:    header + "02" + user + "0035" + "02" + "0b" + "6578616d706c652e636f6d",
		},
		{
			name:    "ipv4 host",
			host:    "203.0.113.10",
			port:    39281,
			command: x365CommandTCP,
			want:    header + "01" + user + "9971" + "01" + "cb00710a",
		},
		{
			name:    "ipv6 host",
			host:    "2001:db8::1",
			port:    443,
			command: x365CommandTCP,
			want:    header + "01" + user + "01bb" + "03" + "20010db8000000000000000000000001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildX365Request(tt.host, tt.port, userID, tt.command)
			if err != nil {
				t.Fatal(err)
			}
			if gotHex := hex.EncodeToString(got); gotHex != tt.want {
				t.Fatalf("request mismatch\nwant %s\n got %s", tt.want, gotHex)
			}
		})
	}
}

func TestBuildX365RequestRejectsBadInput(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		port    int
		command x365Command
	}{
		{name: "unknown command", port: 53, command: x365Command(9)},
		{name: "negative port", port: -1, command: x365CommandTCP},
		{name: "port overflow", port: 65536, command: x365CommandTCP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildX365Request("example.com", tt.port, userID, tt.command); err == nil {
				t.Fatal("request build succeeded, want error")
			}
		})
	}
}

func TestParseUserID(t *testing.T) {
	got, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString("00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:], want) {
		t.Fatalf("user id=%x, want %x", got, want)
	}
}

func TestParseUserIDRejectsBadLengthAndHex(t *testing.T) {
	for _, uuidText := range []string{
		"00112233-4455-6677-8899-aabbccddee",
		"00112233-4455-6677-8899-aabbccddeeff00",
		"00112233-4455-6677-8899-aabbccddeegg",
	} {
		if _, err := ParseUserID(uuidText); err == nil {
			t.Fatalf("ParseUserID(%q) succeeded, want error", uuidText)
		}
	}
}
