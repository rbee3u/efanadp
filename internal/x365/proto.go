package x365

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strings"
)

const (
	multipathControlHost = "multipath.internal"
	multipathControlPort = 1
)

const x365RequestVersion byte = 1

type x365Command byte

const (
	x365CommandTCP x365Command = 1
	x365CommandUDP x365Command = 2
)

// UserID is the private 16-byte account value carried in X365 requests.
type UserID [16]byte

// ParseUserID decodes dashed or undashed hexadecimal UUID text.
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

// X365 address types differ from SOCKS5: 0x01 IPv4, 0x02 domain, 0x03 IPv6.
// See docs/x365-protocol.md for the request layout.
func buildX365Request(destHost string, destPort int, userID UserID, command x365Command) ([]byte, error) {
	if destPort < 0 || destPort > 65535 {
		return nil, fmt.Errorf("destination port out of range: %d", destPort)
	}
	switch command {
	case x365CommandTCP, x365CommandUDP:
	default:
		return nil, fmt.Errorf("unsupported X365 command: %d", command)
	}
	request := make([]byte, 0, 64+len(destHost))
	request = append(request, 'X', '3', '6', '5', x365RequestVersion, byte(command))
	request = append(request, userID[:]...)
	request = binary.BigEndian.AppendUint16(request, uint16(destPort))

	if ip := net.ParseIP(destHost); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			request = append(request, 1)
			request = append(request, v4...)
			return request, nil
		}
		v6 := ip.To16()
		if v6 == nil {
			return nil, fmt.Errorf("invalid IP address: %s", destHost)
		}
		request = append(request, 3)
		request = append(request, v6...)
		return request, nil
	}

	hostLen := len(destHost)
	if hostLen > 255 {
		return nil, fmt.Errorf("domain name too long: %d", hostLen)
	}
	request = append(request, 2, byte(hostLen))
	request = append(request, destHost...)
	return request, nil
}

func readX365OKResponse(r io.Reader) error {
	var resp [5]byte
	if _, err := io.ReadFull(r, resp[:]); err != nil {
		return err
	}
	if string(resp[:4]) != "X365" || resp[4] != 0 {
		return fmt.Errorf("bad X365 response: %x", resp)
	}
	return nil
}
