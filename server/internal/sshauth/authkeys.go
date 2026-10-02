package sshauth

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// ErrNotAuthorized means no usable authorized_keys line lists the key.
var ErrNotAuthorized = errors.New("key not authorized")

// noArgOptions are authorized_keys options without a value that do not
// restrict a web sign-in (they only limit what an SSH session may do).
var noArgOptions = map[string]bool{
	"restrict": true, "no-pty": true, "pty": true, "no-port-forwarding": true, "port-forwarding": true,
	"no-agent-forwarding": true, "agent-forwarding": true, "no-x11-forwarding": true, "x11-forwarding": true,
	"no-user-rc": true, "user-rc": true, "no-touch-required": true, "verify-required": true,
}

// valueOptions are options with a value that are ignored for a web
// sign-in (they configure forwarding or the SSH session environment).
var valueOptions = map[string]bool{
	"environment": true, "permitopen": true, "permitlisten": true, "tunnel": true,
}

// MatchResult describes the authorized_keys line that accepted a key.
type MatchResult struct {
	Line    int
	Comment string
}

// FindKey looks for pub in authorized_keys content. Lines are tried in
// order, like sshd: a line listing the key whose options refuse this
// sign-in is skipped and the search goes on. Options:
//   - cert-authority / principals=: the line is skipped (CA keys and
//     certificates are not supported);
//   - command=: refused (the key is meant for a forced command);
//   - from=: the client address must match the pattern list (CIDR,
//     wildcards, ! negation; host names never match, no DNS is done);
//   - expiry-time=: refused once the time has passed;
//   - forwarding/pty/environment options: ignored;
//   - any other option: the line is skipped (sshd also rejects lines with
//     unknown options).
//
// It returns ErrNotAuthorized, wrapping the reason for the last line that
// listed the key, when no line accepts it.
func FindKey(data []byte, pub ssh.PublicKey, clientIP net.IP, now time.Time) (*MatchResult, error) {
	want := pub.Marshal()
	var lastReason string
	lineNo := 0
	for _, raw := range bytes.Split(data, []byte("\n")) {
		lineNo++
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		key, comment, opts, _, err := ssh.ParseAuthorizedKey(line)
		if err != nil {
			continue
		}
		if !bytes.Equal(key.Marshal(), want) {
			continue
		}
		if reason := checkOptions(opts, clientIP, now); reason != "" {
			lastReason = fmt.Sprintf("line %d: %s", lineNo, reason)
			continue
		}
		return &MatchResult{Line: lineNo, Comment: comment}, nil
	}
	if lastReason != "" {
		return nil, fmt.Errorf("%w: %s", ErrNotAuthorized, lastReason)
	}
	return nil, ErrNotAuthorized
}

// checkOptions returns "" when the options allow a web sign-in from ip.
func checkOptions(opts []string, ip net.IP, now time.Time) string {
	for _, o := range opts {
		name, val, hasVal := strings.Cut(o, "=")
		name = strings.ToLower(strings.TrimSpace(name))
		if hasVal {
			v, ok := unquote(val)
			if !ok {
				return fmt.Sprintf("malformed option %q", name)
			}
			val = v
		}
		switch {
		case name == "cert-authority" && !hasVal:
			return "cert-authority lines are not supported"
		case name == "principals" && hasVal:
			return "principals= (certificates) is not supported"
		case name == "command" && hasVal:
			return "the key has a forced command (command=)"
		case name == "from" && hasVal:
			if !MatchFrom(val, ip) {
				return fmt.Sprintf("client address %s does not match from=%q", ip, val)
			}
		case name == "expiry-time" && hasVal:
			t, err := parseExpiry(val, now.Location())
			if err != nil {
				return fmt.Sprintf("invalid expiry-time %q", val)
			}
			if !now.Before(t) {
				return fmt.Sprintf("the key expired on %s", t.Format(time.RFC3339))
			}
		case !hasVal && noArgOptions[name]:
		case hasVal && valueOptions[name]:
		default:
			return fmt.Sprintf("unsupported option %q", name)
		}
	}
	return ""
}

// unquote strips the double quotes of an option value and undoes \"
// escapes. Unquoted values (from=1.2.3.4) are accepted as they are.
func unquote(v string) (string, bool) {
	if !strings.HasPrefix(v, `"`) {
		return v, !strings.ContainsAny(v, ` "`)
	}
	if len(v) < 2 || !strings.HasSuffix(v, `"`) {
		return "", false
	}
	v = v[1 : len(v)-1]
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) && v[i+1] == '"' {
			b.WriteByte('"')
			i++
			continue
		}
		if v[i] == '"' {
			return "", false
		}
		b.WriteByte(v[i])
	}
	return b.String(), true
}

// MatchFrom applies an sshd from= pattern list to ip: a matching negated
// pattern (!…) refuses, otherwise one matching pattern accepts. Patterns are
// CIDR blocks or wildcard patterns on the address text, where only * and ?
// are special (as in sshd's match_pattern; [ and \ are literal). Host names
// are never resolved, so name patterns do not match. A malformed CIDR block
// refuses the whole list, as sshd does (otherwise "!10.0.0.0/33,*" would
// accept every address).
func MatchFrom(list string, ip net.IP) bool {
	if ip == nil {
		return false
	}
	addr := ip.String()
	if v4 := ip.To4(); v4 != nil {
		addr = v4.String()
	}
	ok := false
	for _, p := range strings.Split(list, ",") {
		p = strings.TrimSpace(p)
		neg := strings.HasPrefix(p, "!")
		p = strings.TrimPrefix(p, "!")
		if p == "" {
			continue
		}
		var m bool
		if strings.Contains(p, "/") {
			_, n, err := net.ParseCIDR(p)
			if err != nil {
				return false
			}
			m = n.Contains(ip)
		} else {
			m = wildcardMatch(strings.ToLower(p), strings.ToLower(addr))
			if !m && strings.Contains(p, ":") {
				// IPv6 written differently ("::1" vs "0:0::1").
				if pip := net.ParseIP(p); pip != nil {
					m = pip.Equal(ip)
				}
			}
		}
		if m && neg {
			return false
		}
		if m {
			ok = true
		}
	}
	return ok
}

// wildcardMatch is sshd's match_pattern: '*' matches any run of bytes,
// '?' any one byte, everything else itself.
func wildcardMatch(pat, s string) bool {
	px, sx := 0, 0
	starP, starS := -1, 0
	for sx < len(s) {
		switch {
		case px < len(pat) && pat[px] == '*':
			starP, starS = px, sx
			px++
		case px < len(pat) && (pat[px] == '?' || pat[px] == s[sx]):
			px++
			sx++
		case starP >= 0:
			starS++
			px, sx = starP+1, starS
		default:
			return false
		}
	}
	for px < len(pat) && pat[px] == '*' {
		px++
	}
	return px == len(pat)
}

// parseExpiry parses sshd's expiry-time: YYYYMMDD[HHMM[SS]], in local
// time unless suffixed with Z (UTC).
func parseExpiry(v string, loc *time.Location) (time.Time, error) {
	if strings.HasSuffix(v, "Z") || strings.HasSuffix(v, "z") {
		v = v[:len(v)-1]
		loc = time.UTC
	}
	var layout string
	switch len(v) {
	case 8:
		layout = "20060102"
	case 12:
		layout = "200601021504"
	case 14:
		layout = "20060102150405"
	default:
		return time.Time{}, errors.New("bad length")
	}
	return time.ParseInLocation(layout, v, loc)
}
