package x365

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

// Multipath frame header layout: one type byte, an 8-byte sequence number, one
// reserved byte, and a 3-byte payload length.
const (
	frameHeaderSize = 13
	frameMaxPayload = 0xffffff
)

// Multipath frame types, as carried in the first header byte.
const (
	frameHello byte = 0
	frameData  byte = 1
	frameClose byte = 3
	frameACK   byte = 5
)

type frame struct {
	Type     byte
	Seq      uint64
	Reserved byte
	Payload  []byte
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

// writeFrameHeader writes a 13-byte header. Callers bound payloadLen to 24
// bits, so the high byte written at dst[9] is the reserved zero byte.
func writeFrameHeader(dst []byte, frameType byte, seq uint64, payloadLen uint32) {
	dst[0] = frameType
	binary.BigEndian.PutUint64(dst[1:9], seq)
	binary.BigEndian.PutUint32(dst[9:13], payloadLen)
}

func parseFrameHeader(buf []byte) (frame, int) {
	payloadLen := int(buf[10])<<16 | int(buf[11])<<8 | int(buf[12])
	seq := binary.BigEndian.Uint64(buf[1:9])
	return frame{Type: buf[0], Seq: seq, Reserved: buf[9]}, payloadLen
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

type multipathFramer struct {
	StreamID [16]byte
	WriteSeq uint64
}

func newMultipathFramer() (*multipathFramer, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &multipathFramer{StreamID: id}, nil
}

func (f *multipathFramer) openFrame(host string, port int) ([]byte, error) {
	hello, err := buildMultipathHello(host, port, f.StreamID)
	if err != nil {
		return nil, err
	}
	return buildFrame(frameHello, 0, hello)
}

func (f *multipathFramer) dataFrameHeader(payloadLen int) ([frameHeaderSize]byte, error) {
	if payloadLen < 0 || payloadLen > frameMaxPayload {
		return [frameHeaderSize]byte{}, fmt.Errorf("multipath payload too large: %d", payloadLen)
	}
	var header [frameHeaderSize]byte
	writeFrameHeader(header[:], frameData, f.WriteSeq, uint32(payloadLen))
	f.WriteSeq += framePayloadLenUint64(payloadLen)
	return header, nil
}

// framePayloadLenUint64 widens a payload length for sequence arithmetic. Every
// caller has already bounded payloadLen to [0, frameMaxPayload], either by an
// explicit check or because parseFrameHeader builds it from three bytes, so the
// negative branch is unreachable; it stays because it is what makes the
// conversion provably safe to gosec's G115 check.
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

func (f *multipathFramer) closeFrameHeader() [frameHeaderSize]byte {
	var header [frameHeaderSize]byte
	writeFrameHeader(header[:], frameClose, f.WriteSeq, 0)
	return header
}
