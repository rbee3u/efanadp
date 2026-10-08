package proxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

type socksUDPDatagram struct {
	Frag    byte
	Host    string
	Port    int
	Payload []byte
}

func parseSOCKSUDPDatagram(packet []byte) (socksUDPDatagram, error) {
	if len(packet) < 4 {
		return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP packet too short: %d", len(packet))
	}
	if packet[0] != 0 || packet[1] != 0 {
		return socksUDPDatagram{}, errors.New("SOCKS5 UDP reserved bytes are non-zero")
	}
	result := socksUDPDatagram{Frag: packet[2]}
	offset := 4
	switch packet[3] {
	case socksAddrIPv4:
		if len(packet) < offset+net.IPv4len+2 {
			return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP IPv4 packet too short: %d", len(packet))
		}
		result.Host = net.IP(packet[offset : offset+net.IPv4len]).String()
		offset += net.IPv4len
	case socksAddrDomain:
		if len(packet) < offset+1 {
			return socksUDPDatagram{}, errors.New("SOCKS5 UDP domain length missing")
		}
		hostLen := int(packet[offset])
		offset++
		if hostLen == 0 {
			return socksUDPDatagram{}, errors.New("empty SOCKS5 UDP domain")
		}
		if len(packet) < offset+hostLen+2 {
			return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP domain packet too short: %d", len(packet))
		}
		result.Host = string(packet[offset : offset+hostLen])
		offset += hostLen
	case socksAddrIPv6:
		if len(packet) < offset+net.IPv6len+2 {
			return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP IPv6 packet too short: %d", len(packet))
		}
		result.Host = net.IP(packet[offset : offset+net.IPv6len]).String()
		offset += net.IPv6len
	default:
		return socksUDPDatagram{}, fmt.Errorf("unsupported SOCKS5 UDP address type: %d", packet[3])
	}
	if len(packet) < offset+2 {
		return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP port missing: %d", len(packet))
	}
	result.Port = int(binary.BigEndian.Uint16(packet[offset : offset+2]))
	offset += 2
	result.Payload = packet[offset:]
	return result, nil
}

func buildSOCKSUDPDatagram(host string, port int, payload []byte) ([]byte, error) {
	packet := make([]byte, 0, socksUDPHeaderCapacity(host)+len(payload))
	packet, err := appendSOCKSUDPHeader(packet, host, port)
	if err != nil {
		return nil, err
	}
	packet = append(packet, payload...)
	return packet, nil
}

func buildSOCKSUDPHeader(host string, port int) ([]byte, error) {
	header := make([]byte, 0, socksUDPHeaderCapacity(host))
	return appendSOCKSUDPHeader(header, host, port)
}

func socksUDPHeaderCapacity(host string) int {
	return 4 + max(len(host)+1, net.IPv6len) + 2
}

func appendSOCKSUDPHeader(dst []byte, host string, port int) ([]byte, error) {
	dst = append(dst, 0, 0, 0)
	dst, err := appendSOCKSAddress(dst, host, port)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP address: %w", err)
	}
	return dst, nil
}
