package users

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const passwdSample = `root:x:0:0:Super user:/root:/bin/bash
daemon:x:1:1::/usr/bin:/usr/bin/nologin
fonlogen:x:1000:1000:Fonlogen,,,:/home/fonlogen:/usr/bin/bash
mario:x:1001:1001:Mario Rossi:/home/mario:/bin/bash
nobody:x:65534:65534:Kernel Overflow User:/:/usr/bin/nologin
# comment
broken line
`

const groupSample = `root:x:0:
wheel:x:998:fonlogen,mario
docker:x:969:fonlogen
fonlogen:x:1000:
mario:x:1001:
`

func TestParsePasswd(t *testing.T) {
	p := parsePasswd(passwdSample)
	if len(p) != 5 {
		t.Fatalf("got %d entries", len(p))
	}
	if p[2].Name != "fonlogen" || p[2].UID != 1000 || p[2].Home != "/home/fonlogen" || p[2].Shell != "/usr/bin/bash" {
		t.Errorf("bad entry %+v", p[2])
	}
}

func TestParseGroup(t *testing.T) {
	g := parseGroup(groupSample)
	if len(g) != 5 {
		t.Fatalf("got %d groups", len(g))
	}
	if !reflect.DeepEqual(g[1].Members, []string{"fonlogen", "mario"}) || g[1].GID != 998 {
		t.Errorf("bad wheel %+v", g[1])
	}
	if len(g[0].Members) != 0 {
		t.Errorf("root should have no members")
	}
}

func TestParseShadowAndState(t *testing.T) {
	sh := parseShadow("root:!*:19000::::::\nmario:$6$abc$def:20000:0:99999:7:::\nlk:!$6$abc$def:20000::::::\nnew:!:0::::::\nnop::20000::::::\nex:$6$x:20000:0:99999:7::20100:\n")
	cases := map[string]string{"root": "disabled", "mario": "ok", "lk": "locked", "new": "disabled", "nop": "empty", "ex": "ok"}
	for n, want := range cases {
		if got := passwordState(sh[n].Hash); got != want {
			t.Errorf("%s: %s, want %s", n, got, want)
		}
	}
	if sh["new"].LastChange != 0 || sh["mario"].LastChange != 20000 || sh["ex"].Expire != 20100 {
		t.Errorf("bad fields %+v", sh)
	}
}

func TestLoginDefsAndShells(t *testing.T) {
	d := "# x\nUID_MIN\t\t\t 1000\nGID_MIN 500\n#UID_MAX 5\n"
	if loginDefsInt(d, "UID_MIN", 0) != 1000 || loginDefsInt(d, "GID_MIN", 0) != 500 || loginDefsInt(d, "UID_MAX", 7) != 7 {
		t.Error("login.defs parsing")
	}
	sh := parseShells("# c\n/bin/sh\n\n/usr/bin/bash\nnologin\n")
	if !reflect.DeepEqual(sh, []string{"/bin/sh", "/usr/bin/bash"}) {
		t.Errorf("shells %v", sh)
	}
}

func TestSystemClassification(t *testing.T) {
	s := &system{passwd: parsePasswd(passwdSample), groups: parseGroup(groupSample), uidMin: 1000, gidMin: 1000}
	s.byName = map[string]*passwdEntry{}
	for i := range s.passwd {
		s.byName[s.passwd[i].Name] = &s.passwd[i]
	}
	s.groupName = map[string]*groupEntry{}
	for i := range s.groups {
		s.groupName[s.groups[i].Name] = &s.groups[i]
	}
	if !s.isPerson(s.byName["root"]) || !s.isPerson(s.byName["mario"]) || s.isPerson(s.byName["daemon"]) || s.isPerson(s.byName["nobody"]) {
		t.Error("isPerson")
	}
	if !s.isAdmin(s.byName["mario"]) || !s.isAdmin(s.byName["root"]) || s.isAdmin(s.byName["daemon"]) {
		t.Error("isAdmin")
	}
	if s.adminGroup() != "wheel" {
		t.Error("adminGroup")
	}
	if !reflect.DeepEqual(s.supplementary("fonlogen"), []string{"wheel", "docker"}) {
		t.Errorf("supplementary %v", s.supplementary("fonlogen"))
	}
}

func TestValidation(t *testing.T) {
	for _, n := range []string{"mario", "_svc", "a-b_c1", strings.Repeat("a", 32)} {
		if validName("User", n) != nil {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", "Mario", "1abc", "-x", "a b", "a:b", "a/b", "a;rm", strings.Repeat("a", 33), "ünï", "a\n"} {
		if validName("User", n) == nil {
			t.Errorf("%q should be invalid", n)
		}
	}
	if validFullName("Mario Rossi") != nil || validFullName("a:b") == nil || validFullName("a,b") == nil || validFullName("a\nb") == nil {
		t.Error("full name")
	}
	if validPassword("") == nil || validPassword("a\nb") == nil || validPassword("pässword 1") != nil {
		t.Error("password")
	}
}

func TestBuildUseradd(t *testing.T) {
	got := buildUseradd(createParams{Name: "bob", FullName: "Bob B", Shell: "/bin/bash"}, []string{"wheel", "docker"}, true)
	want := []string{"-c", "Bob B", "-s", "/bin/bash", "-m", "-G", "wheel,docker", "bob"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
	got = buildUseradd(createParams{Name: "bob", Shell: "/bin/sh"}, nil, false)
	if !reflect.DeepEqual(got, []string{"-c", "", "-s", "/bin/sh", "-M", "bob"}) {
		t.Errorf("%v", got)
	}
}

// ---- keys ----

func testKey(typ string) string {
	blob := []byte{0, 0, 0, byte(len(typ))}
	blob = append(blob, typ...)
	blob = append(blob, 0, 0, 0, 4, 1, 2, 3, 4)
	return base64.StdEncoding.EncodeToString(blob)
}

func TestParseKeyLine(t *testing.T) {
	b := testKey("ssh-ed25519")
	blob, _ := base64.StdEncoding.DecodeString(b)
	sum := sha256.Sum256(blob)
	fp := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])

	k, ok, err := parseKeyLine("ssh-ed25519 " + b + " laptop thinkpad")
	if !ok || err != nil || k.Type != "ssh-ed25519" || k.Comment != "laptop thinkpad" || k.Fingerprint != fp || k.Options != "" {
		t.Fatalf("%+v %v %v", k, ok, err)
	}
	k, ok, err = parseKeyLine(`command="echo a b",no-pty ssh-ed25519 ` + b)
	if !ok || err != nil || k.Options != `command="echo a b",no-pty` || k.Comment != "" {
		t.Fatalf("options: %+v %v", k, err)
	}
	if _, ok, _ := parseKeyLine("# comment"); ok {
		t.Error("comment is not a key")
	}
	if _, ok, _ := parseKeyLine("   "); ok {
		t.Error("blank is not a key")
	}
	for _, bad := range []string{
		"ssh-ed25519", "ssh-ed25519 !!!notbase64", "ssh-foo " + b, "ssh-rsa " + b, // type mismatch inside blob
		"hello world", "no-pty", "ssh-ed25519 AAAA",
	} {
		if _, ok, err := parseKeyLine(bad); !ok || err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestAuthorizedKeysEditing(t *testing.T) {
	b1, b2 := testKey("ssh-ed25519"), testKey("ssh-rsa")
	data := "# my keys\nssh-ed25519 " + b1 + " one\n\nbogus line\nssh-rsa " + b2 + " two\n"
	keys := parseAuthorizedKeys(data)
	if len(keys) != 2 || keys[0].Line != 2 || keys[1].Line != 5 {
		t.Fatalf("%+v", keys)
	}
	out, n := removeKey(data, keys[0].Fingerprint)
	if n != 1 || out != "# my keys\n\nbogus line\nssh-rsa "+b2+" two\n" {
		t.Errorf("%d %q", n, out)
	}
	if _, n := removeKey(data, "SHA256:nope"); n != 0 {
		t.Error("removed something")
	}
	if got := appendKey("a", "b"); got != "a\nb\n" {
		t.Errorf("%q", got)
	}
	if got := appendKey("", "b"); got != "b\n" {
		t.Errorf("%q", got)
	}
}

func TestKeyFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "authorized_keys")
	if s, err := readKeyFile(path); err != nil || s != "" {
		t.Fatalf("missing file: %q %v", s, err)
	}
	if err := writeKeyFile(path, "ssh-ed25519 "+testKey("ssh-ed25519")+" x\n"); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	di, _ := os.Stat(filepath.Dir(path))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("modes %v %v", fi.Mode(), di.Mode())
	}
	if _, err := os.Stat(filepath.Join(dir, ".ssh", ".authorized_keys.tmp")); err == nil {
		t.Error("temp file left behind")
	}
	// a symlink must not be followed when reading
	link := filepath.Join(dir, "link")
	os.Symlink(path, link)
	if _, err := readKeyFile(link); err == nil {
		t.Error("symlink followed")
	}
}

// ---- lastlog / last / sessions ----

const lastlog2Sample = `Username         Port     From                                       Latest 
fonlogen         tty2                                                Thu Oct  1 18:22:06 +0200 2026
mario            pts/1    192.168.1.20                               Wed Sep 30 21:40:00 +0200 2026
root                                                                 **Never logged in**
`

func TestParseLastlog(t *testing.T) {
	seen, never := parseLastlog(lastlog2Sample)
	if len(seen) != 2 || !never["root"] {
		t.Fatalf("%v %v", seen, never)
	}
	if m := seen["mario"]; m.Tty != "pts/1" || m.From != "192.168.1.20" || m.At == 0 {
		t.Errorf("%+v", m)
	}
	if f := seen["fonlogen"]; f.Tty != "tty2" || f.From != "" {
		t.Errorf("%+v", f)
	}
}

const lastSample = `fonlogen pts/0        :0               2026-10-01T18:22:11+02:00   still logged in
reboot   system boot  7.2.6-arch2-1    2026-10-01T18:21:25+02:00   still running
mario    pts/1        192.168.1.20     2026-09-30T21:40:12+02:00 - 2026-09-30T22:00:00+02:00  (00:20)
mario    tty1                          2026-09-29T08:00:00+02:00 - 2026-09-29T09:00:00+02:00  (01:00)

wtmp begins 2026-09-22T21:41:19+02:00
`

func TestParseLast(t *testing.T) {
	m := parseLast(lastSample)
	if len(m) != 2 {
		t.Fatalf("%v", m)
	}
	if m["mario"].From != "192.168.1.20" || m["mario"].Tty != "pts/1" {
		t.Errorf("newest login should win: %+v", m["mario"])
	}
	if m["fonlogen"].From != ":0" {
		t.Errorf("%+v", m["fonlogen"])
	}
}

func TestParseSessions(t *testing.T) {
	ids := parseSessionIDs(`[{"session":"2","uid":1000,"user":"fonlogen"},{"session":"c1"}]`)
	if !reflect.DeepEqual(ids, []string{"2", "c1"}) {
		t.Errorf("%v", ids)
	}
	ids = parseSessionIDs("      2 1000 fonlogen seat0 1280   user    tty2 no   -\n")
	if !reflect.DeepEqual(ids, []string{"2"}) {
		t.Errorf("table fallback %v", ids)
	}
	s := parseSessionShow("Id=2\nUser=1000\nName=fonlogen\nTimestamp=Thu 2026-10-01 18:22:06 CEST\nTTY=tty2\nRemote=yes\nRemoteHost=10.0.0.5\nService=sshd\nType=tty\nClass=user\nState=active\n")
	if s.ID != "2" || s.UID != 1000 || s.User != "fonlogen" || !s.Remote || s.Host != "10.0.0.5" || s.Since == 0 || s.Service != "sshd" {
		t.Errorf("%+v", s)
	}
}
