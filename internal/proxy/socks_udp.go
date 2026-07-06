package proxy

import (
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

func parseSocksUDPDatagram(packet []byte) (socksUDPDatagram, error) {
	if len(packet) < 4 {
		return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP packet too short: %d", len(packet))
	}
	if packet[0] != 0 || packet[1] != 0 {
		return socksUDPDatagram{}, errors.New("SOCKS5 UDP reserved bytes are non-zero")
	}
	result := socksUDPDatagram{Frag: packet[2]}
	offset := 4
	switch packet[3] {
	case 1:
		if len(packet) < offset+4+2 {
			return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP IPv4 packet too short: %d", len(packet))
		}
		result.Host = net.IP(packet[offset : offset+4]).String()
		offset += 4
	case 3:
		if len(packet) < offset+1 {
			return socksUDPDatagram{}, errors.New("SOCKS5 UDP domain length missing")
		}
		hostLen := int(packet[offset])
		offset++
		if len(packet) < offset+hostLen+2 {
			return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP domain packet too short: %d", len(packet))
		}
		result.Host = string(packet[offset : offset+hostLen])
		offset += hostLen
	case 4:
		if len(packet) < offset+16+2 {
			return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP IPv6 packet too short: %d", len(packet))
		}
		result.Host = net.IP(packet[offset : offset+16]).String()
		offset += 16
	default:
		return socksUDPDatagram{}, fmt.Errorf("unsupported SOCKS5 UDP address type: %d", packet[3])
	}
	if len(packet) < offset+2 {
		return socksUDPDatagram{}, fmt.Errorf("SOCKS5 UDP port missing: %d", len(packet))
	}
	result.Port = int(packet[offset])<<8 | int(packet[offset+1])
	offset += 2
	result.Payload = packet[offset:]
	return result, nil
}

func buildSocksUDPDatagram(host string, port int, payload []byte) ([]byte, error) {
	packet := make([]byte, 0, socksUDPHeaderCapacity(host)+len(payload))
	packet, err := appendSocksUDPHeader(packet, host, port)
	if err != nil {
		return nil, err
	}
	packet = append(packet, payload...)
	return packet, nil
}

func buildSocksUDPHeader(host string, port int) ([]byte, error) {
	header := make([]byte, 0, socksUDPHeaderCapacity(host))
	return appendSocksUDPHeader(header, host, port)
}

func socksUDPHeaderCapacity(host string) int {
	return 4 + max(len(host)+1, net.IPv6len) + 2
}

func appendSocksUDPHeader(dst []byte, host string, port int) ([]byte, error) {
	dst = append(dst, 0, 0, 0)
	dst, err := appendSocksAddress(dst, host, port)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP address: %w", err)
	}
	return dst, nil
}
