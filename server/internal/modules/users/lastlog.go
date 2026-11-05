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

// lastlogLayout holds the column offsets read from the lastlog header.
type lastlogLayout struct{ port, from, latest int }

func parseLastlogHeader(hdr string) (lastlogLayout, bool) {
	l := lastlogLayout{strings.Index(hdr, "Port"), strings.Index(hdr, "From"), strings.Index(hdr, "Latest")}
	return l, l.port >= 0 && l.from >= 0 && l.latest >= 0
}

// cutColumn returns the trimmed text of l between byte offsets a and b (b < 0 means end of line).
func cutColumn(l string, a, b int) string {
	if a >= len(l) {
		return ""
	}
	if b > len(l) || b < 0 {
		b = len(l)
	}
	return strings.TrimSpace(l[a:b])
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
	layout, ok := parseLastlogHeader(lines[0])
	if !ok {
		return
	}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		name := cutColumn(l, 0, layout.port)
		if name == "" {
			continue
		}
		latest := cutColumn(l, layout.latest, -1)
		if strings.Contains(latest, "Never logged in") || latest == "" {
			never[name] = true
			continue
		}
		t, err := time.Parse("Mon Jan _2 15:04:05 -0700 2006", latest)
		if err != nil {
			continue
		}
		seen[name] = loginInfo{At: t.Unix(), Tty: cutColumn(l, layout.port, layout.from), From: cutColumn(l, layout.from, layout.latest)}
	}
	return
}

var lastSkippedUsers = map[string]bool{"reboot": true, "shutdown": true, "runlevel": true, "wtmp": true}

// parseLastLine reads one `last` row: user tty [host] time ...
func parseLastLine(l string) (user string, info loginInfo, ok bool) {
	f := strings.Fields(l)
	if len(f) < 3 || lastSkippedUsers[f[0]] {
		return "", loginInfo{}, false
	}
	t, err := time.Parse(time.RFC3339, f[2])
	host := ""
	if err != nil {
		if len(f) < 4 {
			return "", loginInfo{}, false
		}
		host = f[2]
		if t, err = time.Parse(time.RFC3339, f[3]); err != nil {
			return "", loginInfo{}, false
		}
	}
	return f[0], loginInfo{At: t.Unix(), Tty: f[1], From: host}, true
}

// parseLast parses `last -w --time-format iso` output and returns the newest
// login per user.
func parseLast(out string) map[string]loginInfo {
	res := map[string]loginInfo{}
	for _, l := range strings.Split(out, "\n") {
		user, info, ok := parseLastLine(l)
		if !ok {
			continue
		}
		if cur, seen := res[user]; seen && cur.At >= info.At {
			continue
		}
		res[user] = info
	}
	return res
}

// mergeNewest copies into dst every login that is newer than the one it holds.
func mergeNewest(dst, src map[string]loginInfo) {
	for u, i := range src {
		if cur, ok := dst[u]; !ok || i.At > cur.At {
			dst[u] = i
		}
	}
}

// lastLogins merges every source that works. known is false when none did.
func lastLogins(ctx context.Context) (logins map[string]loginInfo, never map[string]bool, known bool) {
	logins, never = map[string]loginInfo{}, map[string]bool{}
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
		mergeNewest(logins, seen)
		for u := range nv {
			never[u] = true
		}
	}
	if out, err := sys.Output(ctx, "last", "-w", "--time-format", "iso"); err == nil {
		if m := parseLast(string(out)); len(m) > 0 {
			known = true
			mergeNewest(logins, m)
		}
	}
	for u := range logins {
		delete(never, u)
	}
	return
}
