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

const (
	socksCommandConnect      byte = 1
	socksCommandBind         byte = 2
	socksCommandUDPAssociate byte = 3
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
	if header[0] != 5 {
		return errors.New("not a SOCKS5 greeting")
	}
	var methods [255]byte
	if _, err := io.ReadFull(reader, methods[:header[1]]); err != nil {
		return fmt.Errorf("read SOCKS5 methods: %w", err)
	}
	if !bytes.Contains(methods[:header[1]], []byte{0}) {
		_ = writeFull(writer, []byte{5, 0xff})
		return errors.New("SOCKS5 no-auth method is not offered")
	}
	return writeFull(writer, []byte{5, 0})
}

func socksReadRequest(reader *bufio.Reader, writer io.Writer) (socksRequest, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return socksRequest{}, fmt.Errorf("read SOCKS5 request header: %w", err)
	}
	if header[0] != 5 {
		return socksRequest{}, fmt.Errorf("unsupported SOCKS version: %d", header[0])
	}
	if header[2] != 0 {
		writeSOCKSReplyQuietly(writer, 7)
		return socksRequest{}, fmt.Errorf("unsupported SOCKS5 reserved byte: %d", header[2])
	}
	cmd := header[1]
	if cmd != socksCommandConnect && cmd != socksCommandUDPAssociate {
		writeSOCKSReplyQuietly(writer, 7)
		return socksRequest{}, fmt.Errorf("unsupported SOCKS5 command: %s", socksCommandName(cmd))
	}
	var host string
	switch header[3] {
	case 1:
		var addr [4]byte
		if _, err := io.ReadFull(reader, addr[:]); err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 IPv4 address: %w", err)
		}
		host = net.IP(addr[:]).String()
	case 3:
		sizeByte, err := reader.ReadByte()
		if err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 domain length: %w", err)
		}
		if sizeByte == 0 {
			writeSOCKSReplyQuietly(writer, 8)
			return socksRequest{}, errors.New("empty SOCKS5 domain")
		}
		addr := make([]byte, int(sizeByte))
		if _, err := io.ReadFull(reader, addr); err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 domain: %w", err)
		}
		host = string(addr)
	case 4:
		var addr [16]byte
		if _, err := io.ReadFull(reader, addr[:]); err != nil {
			return socksRequest{}, fmt.Errorf("read SOCKS5 IPv6 address: %w", err)
		}
		host = net.IP(addr[:]).String()
	default:
		writeSOCKSReplyQuietly(writer, 8)
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

func isFakeIPHost(host string) bool {
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return false
	}
	return ip[0] == 198 && (ip[1] == 18 || ip[1] == 19)
}

func writeSocksReply(writer io.Writer, code byte) error {
	return writeSocksReplyAddr(writer, code, nil)
}

func writeSocksReplyAddr(writer io.Writer, code byte, addr net.Addr) error {
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
	reply := []byte{5, code, 0}
	reply, err := appendSocksAddress(reply, host.String(), port)
	if err != nil {
		return err
	}
	return writeFull(writer, reply)
}

func appendSocksAddress(dst []byte, host string, port int) ([]byte, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("SOCKS5 port out of range: %d", port)
	}
	port16 := uint16(port)
	if ip := net.ParseIP(host); ip != nil {
		return appendSocksIP(dst, ip, port16)
	}
	hostLen := len(host)
	if hostLen > 255 {
		return nil, fmt.Errorf("SOCKS5 domain name too long: %d", hostLen)
	}
	dst = append(dst, 3, byte(hostLen))
	dst = append(dst, host...)
	return appendSocksPort(dst, port16), nil
}

func appendSocksIP(dst []byte, ip net.IP, port uint16) ([]byte, error) {
	if v4 := ip.To4(); v4 != nil {
		dst = append(dst, 1)
		dst = append(dst, v4...)
		return appendSocksPort(dst, port), nil
	}
	v6 := ip.To16()
	if v6 == nil {
		return nil, fmt.Errorf("invalid SOCKS5 IP address: %s", ip.String())
	}
	dst = append(dst, 4)
	dst = append(dst, v6...)
	return appendSocksPort(dst, port), nil
}

func appendSocksPort(dst []byte, port uint16) []byte {
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
