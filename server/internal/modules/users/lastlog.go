package users

import (
	"context"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

// loginInfo is the last login of an account.
type loginInfo struct {
	At   int64  `json:"at"`             // unix seconds
	From string `json:"from,omitempty"` // remote host or display
	Tty  string `json:"tty,omitempty"`
}

// parseLastlog parses the table printed by `lastlog` and `lastlog2`:
//
//	Username         Port     From                                       Latest
//	fonlogen         tty2                                                Thu Oct  1 18:22:06 +0200 2026
//	root                                                                 **Never logged in**
//
// Columns are located from the header. The result holds only accounts that
// logged in; never lists the others.
func parseLastlog(out string) (seen map[string]loginInfo, never map[string]bool) {
	seen, never = map[string]loginInfo{}, map[string]bool{}
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return
	}
	hdr := lines[0]
	iPort, iFrom, iLatest := strings.Index(hdr, "Port"), strings.Index(hdr, "From"), strings.Index(hdr, "Latest")
	if iPort < 0 || iFrom < 0 || iLatest < 0 {
		return
	}
	cut := func(l string, a, b int) string {
		if a >= len(l) {
			return ""
		}
		if b > len(l) || b < 0 {
			b = len(l)
		}
		return strings.TrimSpace(l[a:b])
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		name := cut(l, 0, iPort)
		latest := cut(l, iLatest, -1)
		if name == "" {
			continue
		}
		if strings.Contains(latest, "Never logged in") || latest == "" {
			never[name] = true
			continue
		}
		t, err := time.Parse("Mon Jan _2 15:04:05 -0700 2006", latest)
		if err != nil {
			continue
		}
		seen[name] = loginInfo{At: t.Unix(), Tty: cut(l, iPort, iFrom), From: cut(l, iFrom, iLatest)}
	}
	return
}

// parseLast parses `last -w --time-format iso` output and returns the newest
// login per user.
func parseLast(out string) map[string]loginInfo {
	res := map[string]loginInfo{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || f[0] == "reboot" || f[0] == "shutdown" || f[0] == "runlevel" || f[0] == "wtmp" {
			continue
		}
		// user tty [host] time ...
		ti, host := 2, ""
		t, err := time.Parse(time.RFC3339, f[ti])
		if err != nil {
			if len(f) < 4 {
				continue
			}
			host = f[2]
			ti = 3
			if t, err = time.Parse(time.RFC3339, f[ti]); err != nil {
				continue
			}
		}
		if cur, ok := res[f[0]]; ok && cur.At >= t.Unix() {
			continue
		}
		res[f[0]] = loginInfo{At: t.Unix(), Tty: f[1], From: host}
	}
	return res
}

// lastLogins merges every source that works. known is false when none did.
func lastLogins(ctx context.Context) (logins map[string]loginInfo, never map[string]bool, known bool) {
	logins, never = map[string]loginInfo{}, map[string]bool{}
	merge := func(m map[string]loginInfo) {
		for u, i := range m {
			if cur, ok := logins[u]; !ok || i.At > cur.At {
				logins[u] = i
			}
		}
	}
	for _, name := range []string{"lastlog2", "lastlog"} {
		out, err := sys.Output(ctx, name)
		if err != nil {
			continue
		}
		seen, nv := parseLastlog(string(out))
		if len(seen) == 0 && len(nv) == 0 {
			continue
		}
		known = true
		merge(seen)
		for u := range nv {
			never[u] = true
		}
	}
	if out, err := sys.Output(ctx, "last", "-w", "--time-format", "iso"); err == nil {
		if m := parseLast(string(out)); len(m) > 0 {
			known = true
			merge(m)
		}
	}
	for u := range logins {
		delete(never, u)
	}
	return
}
