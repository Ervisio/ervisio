package config

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

// ParseOrigin validates a browser origin ("https://host[:port]") and returns
// it as scheme + "://" + lower-case host with the default port removed.
func ParseOrigin(s string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("must start with https:// or http://")
	}
	if u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("must be only scheme, host and optional port")
	}
	return u.Scheme + "://" + NormalizeHost(u.Host, u.Scheme), nil
}

// NormalizeHost lower-cases host[:port] and drops the scheme's default port.
func NormalizeHost(host, scheme string) string {
	h := strings.ToLower(host)
	switch {
	case scheme == "https" && strings.HasSuffix(h, ":443"):
		h = strings.TrimSuffix(h, ":443")
	case scheme == "http" && strings.HasSuffix(h, ":80"):
		h = strings.TrimSuffix(h, ":80")
	}
	return h
}

// ParsePrefix accepts an IP address or a CIDR.
func ParsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
