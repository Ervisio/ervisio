package sys

import (
	"net"
	"os"
)

// Hostname returns the host name ("localhost" if unknown).
func Hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "localhost"
	}
	return h
}

// PrimaryIP returns the address the host would use for outbound traffic,
// falling back to the first global unicast interface address. It sends no
// packets. Returns "" when the host has no usable address.
func PrimaryIP() string {
	for _, target := range []string{"192.0.2.1:9", "[2001:db8::1]:9"} {
		if c, err := net.Dial("udp", target); err == nil {
			ip := c.LocalAddr().(*net.UDPAddr).IP
			c.Close()
			if ip.IsGlobalUnicast() {
				return ip.String()
			}
		}
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var v6 string
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || !n.IP.IsGlobalUnicast() {
			continue
		}
		if n.IP.To4() != nil {
			return n.IP.String()
		}
		if v6 == "" {
			v6 = n.IP.String()
		}
	}
	return v6
}
