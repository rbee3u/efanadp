package proxy

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

// socksVersion is the only SOCKS version this proxy speaks, sent and expected in
// the VER byte of every greeting, request, and reply (RFC 1928).
const socksVersion byte = 5

// SOCKS5 authentication methods, as negotiated by the greeting (RFC 1928).
const (
	socksAuthNone        byte = 0
	socksAuthUnavailable byte = 0xff
)

const (
	socksCommandConnect      byte = 1
	socksCommandBind         byte = 2
	socksCommandUDPAssociate byte = 3
)

// SOCKS5 address types, as sent in the ATYP byte of a request, a reply, and a
// UDP datagram header (RFC 1928).
const (
	socksAddrIPv4   byte = 1
	socksAddrDomain byte = 3
	socksAddrIPv6   byte = 4
)

// SOCKS5 reply codes for the REP byte of a reply (RFC 1928). The proxy uses
// host-unreachable for a destination it refuses to resolve, connection-refused
// for an upstream that failed to open, and the two "not supported" codes for
// requests it cannot parse or serve.
const (
	socksReplySucceeded           byte = 0
	socksReplyHostUnreachable     byte = 4
	socksReplyConnectionRefused   byte = 5
	socksReplyCommandNotSupported byte = 7
	socksReplyAddrNotSupported    byte = 8
)

type socksRequest struct {
	Command byte
	Host    string
	Port    int
	Address string
}

func socksHandshake(reader *bufio.Reader, writer io.Writer) error {
	var header [2]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return fmt.Errorf("read SOCKS5 greeting: %w", err)
	}
	if header[0] != socksVersion {
		return errors.New("not a SOCKS5 greeting")
	}
	var methods [255]byte
	if _, err := io.ReadFull(reader, methods[:header[1]]); err != nil {
		return fmt.Errorf("read SOCKS5 methods: %w", err)
	}
	if !bytes.Contains(methods[:header[1]], []byte{socksAuthNone}) {
		_ = writeFull(writer, []byte{socksVersion, socksAuthUnavailable})
		return errors.New("SOCKS5 no-auth method is not offered")
	}
	return writeFull(writer, []byte{socksVersion, socksAuthNone})
}

func socksReadRequest(reader *bufio.Reader, writer io.Writer) (socksRequest, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return socksRequest{}, fmt.Errorf("read SOCKS5 request header: %w", err)
	}
	if header[0] != socksVersion {
		return socksRequest{}, fmt.Errorf("unsupported SOCKS version: %d", header[0])
	}
	if header[2] != 0 {
		writeSOCKSReplyQuietly(writer, socksReplyCommandNotSupported)
		return socksRequest{}, fmt.Errorf("unsupported SOCKS5 reserved byte: %d", header[2])
	}
	cmd := header[1]
	if cmd != socksCommandConnect && cmd != socksCommandUDPAssociate {
		writeSOCKSReplyQuietly(writer, socksReplyCommandNotSupported)
		return socksRequest{}, fmt.Errorf("unsupported SOCKS5 command: %s", socksCommandName(cmd))
	}
	var host string
	switch header[3] {
	case socksAddrIPv4:
		var addr [4]byte
		if _, err := io.ReadFull(reader, addr[:]); err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 IPv4 address: %w", err)
		}
		host = net.IP(addr[:]).String()
	case socksAddrDomain:
		sizeByte, err := reader.ReadByte()
		if err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 domain length: %w", err)
		}
		if sizeByte == 0 {
			writeSOCKSReplyQuietly(writer, socksReplyAddrNotSupported)
			return socksRequest{}, errors.New("empty SOCKS5 domain")
		}
		addr := make([]byte, int(sizeByte))
		if _, err := io.ReadFull(reader, addr); err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 domain: %w", err)
		}
		host = string(addr)
	case socksAddrIPv6:
		var addr [16]byte
		if _, err := io.ReadFull(reader, addr[:]); err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 IPv6 address: %w", err)
		}
		host = net.IP(addr[:]).String()
	default:
		writeSOCKSReplyQuietly(writer, socksReplyAddrNotSupported)
		return socksRequest{}, fmt.Errorf("unsupported SOCKS5 address type: %d", header[3])
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(reader, portBytes[:]); err != nil {
		return socksRequest{}, fmt.Errorf("read SOCKS5 port: %w", err)
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])
	return socksRequest{
		Command: cmd,
		Host:    host,
		Port:    port,
		Address: net.JoinHostPort(host, strconv.Itoa(port)),
	}, nil
}

// isFakeIPHost reports whether host is in 198.18.0.0/15, the range mihomo hands
// out for fake-IP DNS. Such an address is only meaningful to the local resolver
// that minted it, so the upstream node cannot map it back to a domain and the
// request is refused rather than relayed to a nonexistent destination.
func isFakeIPHost(host string) bool {
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return false
	}
	return ip[0] == 198 && (ip[1] == 18 || ip[1] == 19)
}

func writeSOCKSReply(writer io.Writer, code byte) error {
	return writeSOCKSReplyAddr(writer, code, nil)
}

func writeSOCKSReplyAddr(writer io.Writer, code byte, addr net.Addr) error {
	host := net.IPv4zero
	port := 0
	switch typed := addr.(type) {
	case *net.TCPAddr:
		host = typed.IP
		port = typed.Port
	case *net.UDPAddr:
		host = typed.IP
		port = typed.Port
	}
	if port < 0 || port > 65535 {
		port = 0
	}
	if host == nil || host.IsUnspecified() {
		host = net.IPv4zero
	}
	reply := []byte{socksVersion, code, 0}
	reply, err := appendSOCKSAddress(reply, host.String(), port)
	if err != nil {
		return err
	}
	return writeFull(writer, reply)
}

func appendSOCKSAddress(dst []byte, host string, port int) ([]byte, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("SOCKS5 port out of range: %d", port)
	}
	port16 := uint16(port)
	if ip := net.ParseIP(host); ip != nil {
		return appendSOCKSIP(dst, ip, port16)
	}
	hostLen := len(host)
	if hostLen > 255 {
		return nil, fmt.Errorf("SOCKS5 domain name too long: %d", hostLen)
	}
	dst = append(dst, socksAddrDomain, byte(hostLen))
	dst = append(dst, host...)
	return appendSOCKSPort(dst, port16), nil
}

func appendSOCKSIP(dst []byte, ip net.IP, port uint16) ([]byte, error) {
	if v4 := ip.To4(); v4 != nil {
		dst = append(dst, socksAddrIPv4)
		dst = append(dst, v4...)
		return appendSOCKSPort(dst, port), nil
	}
	v6 := ip.To16()
	if v6 == nil {
		return nil, fmt.Errorf("invalid SOCKS5 IP address: %s", ip.String())
	}
	dst = append(dst, socksAddrIPv6)
	dst = append(dst, v6...)
	return appendSOCKSPort(dst, port), nil
}

func appendSOCKSPort(dst []byte, port uint16) []byte {
	return binary.BigEndian.AppendUint16(dst, port)
}

func socksCommandName(cmd byte) string {
	switch cmd {
	case socksCommandConnect:
		return "CONNECT(1)"
	case socksCommandBind:
		return "BIND(2)"
	case socksCommandUDPAssociate:
		return "UDP_ASSOCIATE(3)"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", cmd)
	}
}
