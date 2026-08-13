package x365

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func sameHost(left, right string) bool {
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	if leftIP != nil || rightIP != nil {
		return leftIP != nil && rightIP != nil && leftIP.Equal(rightIP)
	}
	return strings.EqualFold(left, right)
}

func parseShadowsocksTargetAddress(payload []byte) (string, bool) {
	if len(payload) < 1 {
		return "", false
	}
	var host string
	var portOffset int
	switch payload[0] {
	case 1:
		if len(payload) < 1+net.IPv4len+2 {
			return "", false
		}
		host = net.IP(payload[1 : 1+net.IPv4len]).String()
		portOffset = 1 + net.IPv4len
	case 3:
		if len(payload) < 2 {
			return "", false
		}
		hostLen := int(payload[1])
		if hostLen == 0 || len(payload) < 2+hostLen+2 {
			return "", false
		}
		host = string(payload[2 : 2+hostLen])
		portOffset = 2 + hostLen
	case 4:
		if len(payload) < 1+net.IPv6len+2 {
			return "", false
		}
		host = net.IP(payload[1 : 1+net.IPv6len]).String()
		portOffset = 1 + net.IPv6len
	default:
		return "", false
	}
	port := binary.BigEndian.Uint16(payload[portOffset : portOffset+2])
	if host == "" || port == 0 {
		return "", false
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), true
}

// FormatTargetChain builds a display-only outer->inner target label.
func FormatTargetChain(outer, inner string) string {
	if inner == "" || inner == outer {
		return outer
	}
	return fmt.Sprintf("%s->%s", outer, inner)
}
