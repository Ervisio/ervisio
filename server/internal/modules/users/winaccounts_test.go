package users

import (
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const winSample = `{"computer":"PC1","self":"S-1-5-21-1-2-3-1001","selfName":"mario",
"users":[
{"name":"Administrator","sid":"S-1-5-21-1-2-3-500","fullName":"","description":"Built-in","enabled":false,"passwordRequired":true,"passwordLastSet":0,"lastLogon":0,"accountExpires":0,"profile":""},
{"name":"mario","sid":"S-1-5-21-1-2-3-1001","fullName":"Mario Rossi","enabled":true,"passwordRequired":true,"passwordLastSet":1700000000,"lastLogon":1700001000,"accountExpires":0,"profile":"C:\\Users\\mario"},
{"name":"anna","sid":"S-1-5-21-1-2-3-1002","fullName":"","enabled":false,"passwordRequired":true,"passwordLastSet":0,"lastLogon":0,"accountExpires":0,"profile":""}],
"groups":[
{"name":"Administrators","sid":"S-1-5-32-544","description":"Admins","members":[{"name":"PC1\\Administrator","sid":"S-1-5-21-1-2-3-500"},{"name":"PC1\\mario","sid":"S-1-5-21-1-2-3-1001"}]},
{"name":"Users","sid":"S-1-5-32-545","description":"","members":{"name":"PC1\\anna","sid":"S-1-5-21-1-2-3-1002"}},
{"name":"Team","sid":"S-1-5-21-1-2-3-1100","description":"","members":[]}]}`

func TestWinSnapshotMapping(t *testing.T) {
	s, err := parseWinSnapshot([]byte("\xef\xbb\xbf" + winSample))
	if err != nil {
		t.Fatal(err)
	}
	r := s.listResult(time.Unix(1700000000+3*86400, 0))
	people := r["people"].([]userInfo)
	system := r["system"].([]userInfo)
	if len(people) != 3 || len(system) != 0 {
		t.Fatalf("people=%d system=%d", len(people), len(system))
	}
	if people[0].Name != "Administrator" || people[0].UID != 500 || !people[0].IsAdmin {
		t.Errorf("admin: %+v", people[0])
	}
	m := people[1]
	if m.UID != 1001 || m.SID != "S-1-5-21-1-2-3-1001" || !m.IsAdmin || m.Home != `C:\Users\mario` ||
		*m.Locked || m.PasswordChanged == nil || *m.PasswordChanged != 3 || m.LastLogin == nil || r["self"] != "mario" {
		t.Errorf("mario: %+v", m)
	}
	a := people[2]
	if !*a.Locked || a.PasswordState != "disabled" || len(a.Groups) != 1 || a.Groups[0] != "Users" || !a.NeverLoggedIn {
		t.Errorf("anna: %+v", a)
	}
	g := s.groupsResult()
	if len(g) != 3 || g[0].GID != 544 || !g[0].System || g[2].System || g[0].Members[1] != "mario" {
		t.Errorf("groups: %+v", g)
	}
	if s.otherAdmins("mario") != 0 { // Administrator is disabled
		t.Errorf("otherAdmins = %d", s.otherAdmins("mario"))
	}
}

func TestFlexList(t *testing.T) {
	var l flexList[winMember]
	for in, n := range map[string]int{`null`: 0, `[]`: 0, `{"name":"a"}`: 1, `[{"name":"a"},{"name":"b"}]`: 2} {
		if err := l.UnmarshalJSON([]byte(in)); err != nil || len(l) != n {
			t.Errorf("%s: %v %d", in, err, len(l))
		}
	}
}

func TestWinNames(t *testing.T) {
	for _, n := range []string{"mario", "Mario Rossi", "a.b", "ünï"} {
		if err := validWinUser(n); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []string{"", " x", "a/b", `a\b`, "a:b", "...", strings.Repeat("a", 21), "a\nb", "a@b"} {
		if validWinUser(n) == nil {
			t.Errorf("%q accepted", n)
		}
	}
}

func TestAsciiJSON(t *testing.T) {
	b, _ := asciiJSON(map[string]string{"p": "pä\"😀"})
	if string(b) != `{"p":"p\u00e4\"\ud83d\ude00"}` {
		t.Errorf("got %s", b)
	}
}

func TestClassifyPSError(t *testing.T) {
	cases := map[string]rpc.Code{
		"ERR|AccessDenied,Microsoft.PowerShell.Commands.NewLocalUserCommand|System.UnauthorizedAccessException|Access is denied": rpc.NeedsAdmin,
		"ERR|UserExists,X|Microsoft.PowerShell.Commands.UserExistsException|The user already exists.":                            rpc.Conflict,
		"ERR|GroupNotFound,X|Microsoft.PowerShell.Commands.GroupNotFoundException|Group x was not found.":                        rpc.NotFound,
		"ERR|InvalidPasswordException,X|Microsoft.PowerShell.Commands.InvalidPasswordException|bad":                              rpc.Invalid,
		"ERR|Other,X|System.Exception|boom": rpc.Internal,
	}
	for in, want := range cases {
		if err := classifyPSError("x", in+"\n"); !rpc.IsCode(err, want) {
			t.Errorf("%q: %v", in, err)
		}
	}
}
