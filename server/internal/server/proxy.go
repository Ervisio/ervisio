package server

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/Fonlogen/LinuxAdmin/server/internal/config"
)

// fromTrustedProxy reports whether the TCP peer is listed in
// web.trusted_proxies (loopback by default).
func (s *Server) fromTrustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range s.Config().Web.TrustedProxies {
		if pf, err := config.ParsePrefix(p); err == nil && pf.Contains(a) {
			return true
		}
	}
	return false
}

// firstHeaderValue returns the first comma-separated value, trimmed.
func firstHeaderValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// realClientIP is the browser's address: the peer, or for a trusted proxy
// the last X-Forwarded-For hop that is not itself a trusted proxy.
func (s *Server) realClientIP(r *http.Request) string {
	peer := clientIP(r)
	if !s.fromTrustedProxy(r) {
		return peer
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		h := strings.TrimSpace(parts[i])
		a, err := netip.ParseAddr(h)
		if err != nil {
			break
		}
		trusted := false
		for _, p := range s.Config().Web.TrustedProxies {
			if pf, err := config.ParsePrefix(p); err == nil && pf.Contains(a.Unmap()) {
				trusted = true
				break
			}
		}
		if !trusted {
			return a.Unmap().String()
		}
	}
	return peer
}
