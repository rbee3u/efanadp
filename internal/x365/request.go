package x365

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

const (
	multipathControlHost = "multipath.internal"
	multipathControlPort = 1
)

type x365Command byte

const (
	x365CommandTCP x365Command = 1
	x365CommandUDP x365Command = 2
)

type UserID [16]byte

func ParseUserID(uuidText string) (UserID, error) {
	cleaned := strings.ReplaceAll(uuidText, "-", "")
	raw, err := hex.DecodeString(cleaned)
	if err != nil {
		return UserID{}, err
	}
	if len(raw) != len(UserID{}) {
		return UserID{}, fmt.Errorf("user id must be 16 bytes, got %d", len(raw))
	}
	var userID UserID
	copy(userID[:], raw)
	return userID, nil
}

func buildX365Request(destHost string, destPort int, userID UserID) ([]byte, error) {
	return buildX365RequestWithCommand(destHost, destPort, userID, x365CommandTCP)
}

func buildX365RequestWithCommand(destHost string, destPort int, userID UserID, command x365Command) ([]byte, error) {
	if destPort < 0 || destPort > 65535 {
		return nil, fmt.Errorf("destination port out of range: %d", destPort)
	}
	switch command {
	case x365CommandTCP, x365CommandUDP:
	default:
		return nil, fmt.Errorf("unsupported X365 command: %d", command)
	}
	frame := make([]byte, 0, 64+len(destHost))
	frame = append(frame, 'X', '3', '6', '5', 1, byte(command))
	frame = append(frame, userID[:]...)
	frame = binary.BigEndian.AppendUint16(frame, uint16(destPort))

	if ip := net.ParseIP(destHost); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			frame = append(frame, 1)
			frame = append(frame, v4...)
			return frame, nil
		}
		v6 := ip.To16()
		if v6 == nil {
			return nil, fmt.Errorf("invalid IP address: %s", destHost)
		}
		frame = append(frame, 3)
		frame = append(frame, v6...)
		return frame, nil
	}

	hostLen := len(destHost)
	if hostLen > 255 {
		return nil, fmt.Errorf("domain name too long: %d", hostLen)
	}
	frame = append(frame, 2, byte(hostLen))
	frame = append(frame, destHost...)
	return frame, nil
}
