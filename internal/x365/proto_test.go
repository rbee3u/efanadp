package x365

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

func TestBuildX365MultipathRequest(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	got, err := buildX365Request(multipathControlHost, multipathControlPort, userID)
	if err != nil {
		t.Fatal(err)
	}
	wantHex := "58333635010100112233445566778899aabbccddeeff000102126d756c7469706174682e696e7465726e616c"
	if gotHex := hex.EncodeToString(got); gotHex != wantHex {
		t.Fatalf("request mismatch\nwant %s\n got %s", wantHex, gotHex)
	}
}

func TestBuildX365UDPRequest(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	got, err := buildX365RequestWithCommand("example.com", 53, userID, x365CommandUDP)
	if err != nil {
		t.Fatal(err)
	}
	wantHex := "58333635010200112233445566778899aabbccddeeff0035020b6578616d706c652e636f6d"
	if gotHex := hex.EncodeToString(got); gotHex != wantHex {
		t.Fatalf("request mismatch\nwant %s\n got %s", wantHex, gotHex)
	}
}

func TestBuildX365RequestRejectsBadCommand(t *testing.T) {
	userID, err := ParseUserID("00112233-4455-6677-8899-aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildX365RequestWithCommand("example.com", 53, userID, x365Command(9)); err == nil {
		t.Fatal("expected bad command error")
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

func TestMultipathHelloRoundTrip(t *testing.T) {
	var streamID [16]byte
	copy(streamID[:], bytes.Repeat([]byte{0x11}, 16))
	payload, err := buildMultipathHello("example.com", 443, streamID)
	if err != nil {
		t.Fatal(err)
	}
	hello, err := parseHelloForTest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if hello.Host != "example.com" || hello.Port != 443 {
		t.Fatalf("bad hello: %+v", hello)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	frameBytes, err := buildFrame(frameData, 0x0102030405060708, []byte{0x16, 0x03, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	parsed, consumed, err := parseFrame(frameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != len(frameBytes) {
		t.Fatalf("consumed %d, want %d", consumed, len(frameBytes))
	}
	if parsed.Type != frameData || parsed.Seq != 0x0102030405060708 || !bytes.Equal(parsed.Payload, []byte{0x16, 0x03, 0x01}) {
		t.Fatalf("bad frame: %+v", parsed)
	}
}

func TestMultipathFramerSequence(t *testing.T) {
	var streamID [16]byte
	framer := &multipathFramer{StreamID: streamID}
	first, err := framer.dataFrameHeader(3)
	if err != nil {
		t.Fatal(err)
	}
	second, err := framer.dataFrameHeader(2)
	if err != nil {
		t.Fatal(err)
	}
	parsedFirst, firstPayloadLen := parseFrameHeader(first[:])
	parsedSecond, secondPayloadLen := parseFrameHeader(second[:])
	if parsedFirst.Seq != 0 || firstPayloadLen != 3 || parsedSecond.Seq != 3 || secondPayloadLen != 2 {
		t.Fatalf(
			"bad sequence: first seq=%d len=%d second seq=%d len=%d",
			parsedFirst.Seq,
			firstPayloadLen,
			parsedSecond.Seq,
			secondPayloadLen,
		)
	}
}

type helloForTest struct {
	StreamID [16]byte
	Host     string
	Port     int
}

func parseHelloForTest(payload []byte) (helloForTest, error) {
	if len(payload) < 21 {
		return helloForTest{}, fmt.Errorf("hello payload too short: %d", len(payload))
	}
	if payload[16] != 0 || payload[17] != 1 {
		return helloForTest{}, fmt.Errorf("bad hello kind/flags: %d/%d", payload[16], payload[17])
	}
	hostLen := int(payload[20])
	end := 21 + hostLen
	if len(payload) != end {
		return helloForTest{}, errors.New("bad hello host length")
	}
	var hello helloForTest
	copy(hello.StreamID[:], payload[:16])
	hello.Port = int(payload[18])<<8 | int(payload[19])
	hello.Host = string(payload[21:end])
	return hello, nil
}
