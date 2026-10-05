package users

// Platform-neutral half of the Windows local-account backend: the JSON that
// the PowerShell scripts print, its mapping onto the users.* shapes, name
// validation and error classification. The process plumbing lives in
// users_windows.go.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const (
	sidAdministrators = "S-1-5-32-544"
	winAdminGroup     = "Administrators"
)

// flexList decodes a JSON array, a single object (what ConvertTo-Json prints
// for one element) or null.
type flexList[T any] []T

func (l *flexList[T]) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "" || t == "null" {
		*l = nil
		return nil
	}
	if t[0] == '[' {
		var s []T
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*l = s
		return nil
	}
	var one T
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*l = []T{one}
	return nil
}

type winUser struct {
	Name            string `json:"name"`
	SID             string `json:"sid"`
	FullName        string `json:"fullName"`
	Description     string `json:"description"`
	Enabled         bool   `json:"enabled"`
	PasswordRequire bool   `json:"passwordRequired"`
	PasswordLastSet int64  `json:"passwordLastSet"` // unix seconds, 0 = never
	LastLogon       int64  `json:"lastLogon"`
	AccountExpires  int64  `json:"accountExpires"` // 0 = never
	Profile         string `json:"profile"`
}

type winMember struct {
	Name string `json:"name"` // COMPUTER\user or DOMAIN\user
	SID  string `json:"sid"`
}

type winGroup struct {
	Name        string              `json:"name"`
	SID         string              `json:"sid"`
	Description string              `json:"description"`
	Members     flexList[winMember] `json:"members"`
}

type winSnapshot struct {
	Computer string             `json:"computer"`
	Self     string             `json:"self"`     // SID of the caller
	SelfName string             `json:"selfName"` // bare name of the caller
	Users    flexList[winUser]  `json:"users"`
	Groups   flexList[winGroup] `json:"groups"`
}

func parseWinSnapshot(out []byte) (*winSnapshot, error) {
	out = []byte(strings.TrimPrefix(strings.TrimSpace(string(out)), "\xef\xbb\xbf"))
	var s winSnapshot
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not read the account list: %v", err)
	}
	return &s, nil
}

// ridOf returns the last numeric component of a SID, or -1.
func ridOf(sid string) int {
	i := strings.LastIndexByte(sid, '-')
	if i < 0 {
		return -1
	}
	n, err := strconv.Atoi(sid[i+1:])
	if err != nil {
		return -1
	}
	return n
}

// isBuiltinGroupSID reports BUILTIN (S-1-5-32-*) groups.
func isBuiltinGroupSID(sid string) bool { return strings.HasPrefix(sid, "S-1-5-32-") }

// localName strips a COMPUTER\ prefix; other domains are returned unchanged.
func localName(full, computer string) string {
	if d, n, ok := strings.Cut(full, `\`); ok && strings.EqualFold(d, computer) {
		return n
	}
	return full
}

func (s *winSnapshot) isPerson(u winUser) bool {
	r := ridOf(u.SID)
	return r >= 1000 || r == 500
}

// groupsOf lists group names the user belongs to; admin reports Administrators.
func (s *winSnapshot) groupsOf(u winUser) (names []string, admin bool) {
	names = []string{}
	for _, g := range s.Groups {
		for _, m := range g.Members {
			if m.SID == u.SID || (m.SID == "" && strings.EqualFold(localName(m.Name, s.Computer), u.Name)) {
				names = append(names, g.Name)
				if g.SID == sidAdministrators {
					admin = true
				}
				break
			}
		}
	}
	sort.Strings(names)
	return
}

func daysAgo(unix int64, now time.Time) *int {
	if unix <= 0 {
		return nil
	}
	d := int(now.Sub(time.Unix(unix, 0)).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return &d
}

func (s *winSnapshot) userInfo(u winUser, now time.Time) userInfo {
	groups, admin := s.groupsOf(u)
	locked := !u.Enabled
	state := "ok"
	switch {
	case !u.Enabled:
		state = "disabled"
	case !u.PasswordRequire && u.PasswordLastSet == 0:
		state = "empty"
	}
	info := userInfo{
		Name: u.Name, SID: u.SID, UID: ridOf(u.SID), FullName: u.FullName,
		Home: u.Profile, Groups: groups, IsAdmin: admin, Locked: &locked,
		PasswordState: state, PasswordChanged: daysAgo(u.PasswordLastSet, now),
		MustChange:   u.Enabled && u.PasswordRequire && u.PasswordLastSet == 0,
		Expired:      u.AccountExpires > 0 && u.AccountExpires < now.Unix(),
		NoLoginShell: true,
	}
	if u.LastLogon > 0 {
		info.LastLogin = &loginInfo{At: u.LastLogon}
	} else {
		info.NeverLoggedIn = true
	}
	return info
}

// listResult builds the users.list reply.
func (s *winSnapshot) listResult(now time.Time) map[string]any {
	people, system := []userInfo{}, []userInfo{}
	self := s.SelfName
	for _, u := range s.Users {
		i := s.userInfo(u, now)
		if s.isPerson(u) {
			people = append(people, i)
		} else {
			system = append(system, i)
		}
		if u.SID == s.Self {
			self = u.Name
		}
	}
	byUID := func(l []userInfo) {
		sort.SliceStable(l, func(a, b int) bool { return l[a].UID < l[b].UID })
	}
	byUID(people)
	byUID(system)
	return map[string]any{
		"people": people, "system": system, "uidMin": 1000,
		"shadowReadable": true, "lastLoginKnown": true,
		"adminGroup": winAdminGroup, "shells": []string{}, "self": self,
	}
}

// groupsResult builds the users.groups reply.
func (s *winSnapshot) groupsResult() []groupInfo {
	out := []groupInfo{}
	for _, g := range s.Groups {
		members := []string{}
		for _, m := range g.Members {
			members = append(members, localName(m.Name, s.Computer))
		}
		out = append(out, groupInfo{
			Name: g.Name, SID: g.SID, GID: ridOf(g.SID), Members: members,
			PrimaryMembers: []string{}, System: isBuiltinGroupSID(g.SID) || ridOf(g.SID) < 1000,
			Description: g.Description,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GID < out[j].GID })
	return out
}

func (s *winSnapshot) user(name string) *winUser {
	for i := range s.Users {
		if strings.EqualFold(s.Users[i].Name, name) {
			return &s.Users[i]
		}
	}
	return nil
}

func (s *winSnapshot) group(name string) *winGroup {
	for i := range s.Groups {
		if strings.EqualFold(s.Groups[i].Name, name) {
			return &s.Groups[i]
		}
	}
	return nil
}

// otherAdmins counts enabled people besides skip in the Administrators group.
func (s *winSnapshot) otherAdmins(skip string) int {
	n := 0
	for _, u := range s.Users {
		if strings.EqualFold(u.Name, skip) || !u.Enabled || !s.isPerson(u) {
			continue
		}
		if _, a := s.groupsOf(u); a {
			n++
		}
	}
	return n
}

// ---- validation ----

const winBadNameChars = "\"/\\[]:;|=,+*?<>@"

func validWinName(kind, n string, max int) error {
	bad := func() error {
		return rpc.Errorf(rpc.Invalid, "%s names have 1 to %d characters and cannot contain control characters or any of %s", kind, max, winBadNameChars)
	}
	if n == "" || len([]rune(n)) > max || strings.TrimRight(n, ". ") == "" || n != strings.TrimSpace(n) {
		return bad()
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(winBadNameChars, r) {
			return bad()
		}
	}
	return nil
}

func validWinUser(n string) error  { return validWinName("User", n, 20) }
func validWinGroup(n string) error { return validWinName("Group", n, 256) }

func validWinText(what, s string, max int) error {
	if len([]rune(s)) > max {
		return rpc.Errorf(rpc.Invalid, "The %s is too long (%d characters at most)", what, max)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return rpc.Errorf(rpc.Invalid, "The %s cannot contain control characters", what)
		}
	}
	return nil
}

// ---- script input / error mapping ----

// asciiJSON marshals v with every non-ASCII character escaped, so the text
// survives the console code page when piped to powershell's stdin.
func asciiJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for _, r := range string(b) {
		if r < 0x80 {
			sb.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			a, c := utf16.EncodeRune(r)
			fmt.Fprintf(&sb, "\\u%04x\\u%04x", a, c)
			continue
		}
		fmt.Fprintf(&sb, "\\u%04x", r)
	}
	return []byte(sb.String()), nil
}

// classifyPSError turns the "ERR|<errorId>|<exception type>|<message>" line the
// scripts print on failure into an rpc error.
func classifyPSError(what, stderr string) error {
	line := stderr
	if i := strings.Index(stderr, "ERR|"); i >= 0 {
		line = stderr[i+4:]
	}
	line = strings.TrimSpace(line)
	parts := strings.SplitN(line, "|", 3)
	id, typ, msg := "", "", line
	if len(parts) == 3 {
		id, typ, msg = strings.ToLower(parts[0]), strings.ToLower(parts[1]), strings.TrimSpace(parts[2])
	}
	low := strings.ToLower(msg)
	has := func(ss ...string) bool {
		for _, s := range ss {
			if strings.Contains(id, s) || strings.Contains(typ, s) || strings.Contains(low, s) {
				return true
			}
		}
		return false
	}
	code := rpc.Internal
	switch {
	case has("accessdenied", "unauthorizedaccess", "access is denied"):
		if msg == "" {
			msg = "access is denied"
		}
		return rpc.Errorf(rpc.NeedsAdmin, "Administrator rights are needed: %s: %s", what, msg)
	case has("userexists", "groupexists", "memberexists", "already exists", "already a member"):
		code = rpc.Conflict
	case has("usernotfound", "groupnotfound", "principalnotfound", "membernotfound", "was not found", "no such"):
		code = rpc.NotFound
	case has("invalidpassword", "does not meet", "password policy", "invalidname", "invalidparameter"):
		code = rpc.Invalid
	}
	if msg == "" {
		msg = "the command failed"
	}
	return rpc.Errorf(code, "%s: %s", what, msg)
}

func winUnavailable(what string) error {
	return rpc.Errorf(rpc.Unavailable, "%s is not available on Windows", what)
}
