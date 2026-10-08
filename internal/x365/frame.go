package x365

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Multipath frame header layout: one type byte, an 8-byte sequence number, one
// reserved byte, and a 3-byte payload length.
const (
	frameHeaderSize = 13
	frameMaxPayload = 0xffffff
)

const (
	frameHello byte = 0
	frameData  byte = 1
	frameClose byte = 3
	frameACK   byte = 5
)

type frameHeader struct {
	Type       byte
	Seq        uint64
	Reserved   byte
	PayloadLen int
}

func buildFrame(frameType byte, seq uint64, payload []byte) ([]byte, error) {
	payloadLen := len(payload)
	if payloadLen > frameMaxPayload {
		return nil, fmt.Errorf("multipath payload too large: %d", payloadLen)
	}
	frame := make([]byte, frameHeaderSize+payloadLen)
	writeFrameHeader(frame[:frameHeaderSize], frameType, seq, uint32(payloadLen))
	copy(frame[frameHeaderSize:], payload)
	return frame, nil
}

// Callers bound payloadLen to 24 bits, leaving dst[9] as the reserved zero byte.
func writeFrameHeader(dst []byte, frameType byte, seq uint64, payloadLen uint32) {
	dst[0] = frameType
	binary.BigEndian.PutUint64(dst[1:9], seq)
	binary.BigEndian.PutUint32(dst[9:13], payloadLen)
}

func readFrameHeader(r io.Reader) (frameHeader, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return frameHeader{}, err
	}
	return parseFrameHeader(header[:]), nil
}

func parseFrameHeader(buf []byte) frameHeader {
	return frameHeader{
		Type:       buf[0],
		Seq:        binary.BigEndian.Uint64(buf[1:9]),
		Reserved:   buf[9],
		PayloadLen: int(buf[10])<<16 | int(buf[11])<<8 | int(buf[12]),
	}
}

func buildMultipathHello(host string, port int, streamID [16]byte) ([]byte, error) {
	if host == "" {
		return nil, errors.New("hello host is required")
	}
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("hello port out of range: %d", port)
	}
	hostLen := len(host)
	if hostLen > 255 {
		return nil, fmt.Errorf("hello host too long: %d", hostLen)
	}
	payload := make([]byte, 0, 21+hostLen)
	payload = append(payload, streamID[:]...)
	payload = append(payload, 0, 1)
	payload = binary.BigEndian.AppendUint16(payload, uint16(port))
	payload = append(payload, byte(hostLen))
	payload = append(payload, host...)
	return payload, nil
}

func buildMultipathOpenFrame(host string, port int, streamID [16]byte) ([]byte, error) {
	hello, err := buildMultipathHello(host, port, streamID)
	if err != nil {
		return nil, err
	}
	return buildFrame(frameHello, 0, hello)
}

func dataFrameHeader(seq uint64, payloadLen int) ([frameHeaderSize]byte, error) {
	if payloadLen < 0 || payloadLen > frameMaxPayload {
		return [frameHeaderSize]byte{}, fmt.Errorf("multipath payload too large: %d", payloadLen)
	}
	var header [frameHeaderSize]byte
	writeFrameHeader(header[:], frameData, seq, uint32(payloadLen))
	return header, nil
}

// Callers bound payloadLen to [0, frameMaxPayload]. The negative guard also
// makes the conversion provably safe to gosec G115.
func framePayloadLenUint64(payloadLen int) uint64 {
	if payloadLen < 0 {
		return 0
	}
	return uint64(payloadLen)
}

func ackFrameHeader(seq uint64) [frameHeaderSize]byte {
	var header [frameHeaderSize]byte
	writeFrameHeader(header[:], frameACK, seq, 0)
	return header
}

func closeFrameHeader(seq uint64) [frameHeaderSize]byte {
	var header [frameHeaderSize]byte
	writeFrameHeader(header[:], frameClose, seq, 0)
	return header
}
