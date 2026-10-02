package users

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/sys"
)

type session struct {
	ID     string `json:"id"`
	User   string `json:"user"`
	UID    int    `json:"uid"`
	Type   string `json:"type"`  // tty, x11, wayland, mir, unspecified
	Class  string `json:"class"` // user, greeter, lock-screen...
	Tty    string `json:"tty"`
	Remote bool   `json:"remote"`
	Host   string `json:"remoteHost"`
	// Service is the login service (sshd, login, gdm...).
	Service string `json:"service"`
	Seat    string `json:"seat"`
	State   string `json:"state"`
	Since   int64  `json:"since"` // unix seconds, 0 when unknown
}

// parseSessionIDs reads `loginctl list-sessions --json=short`; the fallback
// reads the plain table.
func parseSessionIDs(out string) []string {
	var rows []struct {
		Session string `json:"session"`
	}
	if json.Unmarshal([]byte(out), &rows) == nil && out != "" {
		ids := []string{}
		for _, r := range rows {
			if sessionIDRe.MatchString(r.Session) {
				ids = append(ids, r.Session)
			}
		}
		return ids
	}
	ids := []string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && sessionIDRe.MatchString(f[0]) && f[0] != "SESSION" {
			ids = append(ids, f[0])
		}
	}
	return ids
}

// parseSessionShow turns `loginctl show-session` output into a session.
func parseSessionShow(out string) session {
	kv := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			kv[k] = v
		}
	}
	s := session{
		ID: kv["Id"], User: kv["Name"], Type: kv["Type"], Class: kv["Class"], Tty: kv["TTY"],
		Remote: kv["Remote"] == "yes", Host: kv["RemoteHost"], Service: kv["Service"], Seat: kv["Seat"], State: kv["State"],
	}
	s.UID = atoi(kv["User"])
	s.Since = parseSessionTime(kv["Timestamp"])
	return s
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// parseSessionTime reads "Thu 2026-10-01 18:22:06 CEST" in the local zone.
func parseSessionTime(v string) int64 {
	f := strings.Fields(v)
	if len(f) < 3 {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", f[1]+" "+f[2], time.Local)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func listSessions(ctx context.Context, user string) ([]session, error) {
	out, err := sys.Output(ctx, "loginctl", "list-sessions", "--no-legend", "--json=short")
	if err != nil {
		out, err = sys.Output(ctx, "loginctl", "list-sessions", "--no-legend")
		if err != nil {
			return nil, err
		}
	}
	res := []session{}
	for _, id := range parseSessionIDs(string(out)) {
		o, err := sys.Output(ctx, "loginctl", "show-session", id, "--no-pager")
		if err != nil {
			continue // the session ended meanwhile
		}
		s := parseSessionShow(string(o))
		if s.ID == "" || strings.HasPrefix(s.Class, "manager") || (user != "" && s.User != user) {
			continue
		}
		res = append(res, s)
	}
	return res, nil
}
