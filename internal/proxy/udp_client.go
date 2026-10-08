package proxy

import (
	"net"
	"slices"
	"strconv"
)

// Pin the first valid source so another sender cannot capture relay responses.
func (a *udpAssociation) adoptClientAddr(addr *net.UDPAddr) bool {
	if !a.allowsClientAddr(addr) {
		return false
	}
	pinned := a.clientAddr.Load()
	if pinned == nil {
		// Retain an immutable copy independent of the caller.
		copied := *addr
		copied.IP = slices.Clone(addr.IP)
		if a.clientAddr.CompareAndSwap(nil, &copied) {
			return true
		}
		// Another sender won the CAS. The pinned address is never cleared.
		pinned = a.clientAddr.Load()
		if pinned == nil {
			return false
		}
	}
	if sameUDPAddr(pinned, addr) {
		return true
	}
	if a.warnDropOnce(dropReasonForeign) {
		a.cfg.warnf("[udp] drop datagram from=%s reason=%s client=%s", addr, dropReasonForeign, pinned)
	}
	return false
}

// Reject foreign sources before parsing or logging malformed packets. Only a
// valid, allowed datagram may pin an unspecified source port.
func (a *udpAssociation) allowsClientAddr(addr *net.UDPAddr) bool {
	if !matchesExpectedUDPClient(a.expectedClientAddr, addr) {
		if a.warnDropOnce(dropReasonForeign) {
			a.cfg.warnf(
				"[udp] drop datagram from=%s reason=%s client=%s",
				addr,
				dropReasonForeign,
				formatExpectedUDPClient(a.expectedClientAddr),
			)
		}
		return false
	}
	pinned := a.clientAddr.Load()
	if pinned == nil || sameUDPAddr(pinned, addr) {
		return true
	}
	if a.warnDropOnce(dropReasonForeign) {
		a.cfg.warnf("[udp] drop datagram from=%s reason=%s client=%s", addr, dropReasonForeign, pinned)
	}
	return false
}

func expectedUDPClientAddr(remote net.Addr, requestedPort int) *net.UDPAddr {
	tcpAddr, ok := remote.(*net.TCPAddr)
	if !ok || tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		return nil
	}
	return &net.UDPAddr{
		IP:   slices.Clone(tcpAddr.IP),
		Port: requestedPort,
		Zone: tcpAddr.Zone,
	}
}

func matchesExpectedUDPClient(expected, actual *net.UDPAddr) bool {
	if expected == nil {
		return true
	}
	if actual == nil || !expected.IP.Equal(actual.IP) {
		return false
	}
	if expected.Zone != "" && expected.Zone != actual.Zone {
		return false
	}
	return expected.Port == 0 || expected.Port == actual.Port
}

func formatExpectedUDPClient(addr *net.UDPAddr) string {
	if addr == nil {
		return "unknown"
	}
	host := addr.IP.String()
	if addr.Zone != "" {
		host += "%" + addr.Zone
	}
	port := "*"
	if addr.Port != 0 {
		port = strconv.Itoa(addr.Port)
	}
	return net.JoinHostPort(host, port)
}

func sameUDPAddr(left, right *net.UDPAddr) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Port == right.Port && left.Zone == right.Zone && left.IP.Equal(right.IP)
}

// The returned address is shared and must not be mutated.
func (a *udpAssociation) pinnedClientAddr() *net.UDPAddr {
	return a.clientAddr.Load()
}

func udpBindAddrForClient(client net.Conn) *net.UDPAddr {
	ip := net.IPv4(127, 0, 0, 1)
	if tcpAddr, ok := client.LocalAddr().(*net.TCPAddr); ok && tcpAddr.IP != nil && !tcpAddr.IP.IsUnspecified() {
		ip = tcpAddr.IP
	}
	return &net.UDPAddr{IP: ip, Port: 0}
}
