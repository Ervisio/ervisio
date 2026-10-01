package users

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// File locations (variables so tests can point them elsewhere).
var (
	passwdPath    = "/etc/passwd"
	groupPath     = "/etc/group"
	shadowPath    = "/etc/shadow"
	loginDefsPath = "/etc/login.defs"
	shellsPath    = "/etc/shells"
)

// passwdEntry is one line of /etc/passwd.
type passwdEntry struct {
	Name  string
	UID   int
	GID   int
	GECOS string
	Home  string
	Shell string
}

// groupEntry is one line of /etc/group.
type groupEntry struct {
	Name    string
	GID     int
	Members []string
}

// shadowEntry is one line of /etc/shadow.
type shadowEntry struct {
	Name string
	Hash string
	// LastChange is the day (since the epoch) of the last change; -1 when empty.
	LastChange int
	Expire     int // account expiry day, -1 when none
}

func parsePasswd(data string) []passwdEntry {
	var out []passwdEntry
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") || line[0] == '+' || line[0] == '-' {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		uid, e1 := strconv.Atoi(f[2])
		gid, e2 := strconv.Atoi(f[3])
		if e1 != nil || e2 != nil || f[0] == "" {
			continue
		}
		out = append(out, passwdEntry{Name: f[0], UID: uid, GID: gid, GECOS: f[4], Home: f[5], Shell: f[6]})
	}
	return out
}

func parseGroup(data string) []groupEntry {
	var out []groupEntry
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") || line[0] == '+' || line[0] == '-' {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) < 4 {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil || f[0] == "" {
			continue
		}
		g := groupEntry{Name: f[0], GID: gid, Members: []string{}}
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				g.Members = append(g.Members, m)
			}
		}
		out = append(out, g)
	}
	return out
}

func parseShadow(data string) map[string]shadowEntry {
	out := map[string]shadowEntry{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		e := shadowEntry{Name: f[0], Hash: f[1], LastChange: -1, Expire: -1}
		if n, err := strconv.Atoi(f[2]); err == nil {
			e.LastChange = n
		}
		if len(f) > 7 {
			if n, err := strconv.Atoi(f[7]); err == nil {
				e.Expire = n
			}
		}
		out[e.Name] = e
	}
	return out
}

// passwordState classifies a shadow hash field.
//
//	"ok"       a usable password
//	"locked"   a usable password behind a leading "!" (usermod -L)
//	"disabled" no password can ever match ("*", "!", "!*", "!!")
//	"empty"    no password required
func passwordState(hash string) string {
	switch {
	case hash == "":
		return "empty"
	case hash[0] == '*':
		return "disabled"
	case hash[0] == '!':
		rest := strings.TrimLeft(hash, "!")
		if rest == "" || rest[0] == '*' {
			return "disabled"
		}
		return "locked"
	}
	return "ok"
}

// loginDefsInt reads an integer setting from login.defs content.
func loginDefsInt(data, key string, def int) int {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == key {
			if n, err := strconv.Atoi(f[1]); err == nil {
				return n
			}
		}
	}
	return def
}

func parseShells(data string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(data))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' || !strings.HasPrefix(l, "/") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// adminGroups are the groups that grant administrator rights, by preference.
var adminGroups = []string{"wheel", "sudo", "admin"}

// system is a snapshot of the account databases.
type system struct {
	passwd    []passwdEntry
	groups    []groupEntry
	uidMin    int
	gidMin    int
	shells    []string
	shadow    map[string]shadowEntry // nil when unreadable
	byName    map[string]*passwdEntry
	groupName map[string]*groupEntry
}

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func loadSystem(withShadow bool) (*system, error) {
	pw, err := readFile(passwdPath)
	if err != nil {
		return nil, err
	}
	gr, err := readFile(groupPath)
	if err != nil {
		return nil, err
	}
	defs, _ := readFile(loginDefsPath)
	sh, _ := readFile(shellsPath)
	s := &system{
		passwd: parsePasswd(pw),
		groups: parseGroup(gr),
		uidMin: loginDefsInt(defs, "UID_MIN", 1000),
		gidMin: loginDefsInt(defs, "GID_MIN", 1000),
		shells: parseShells(sh),
	}
	if withShadow {
		if b, err := readFile(shadowPath); err == nil {
			s.shadow = parseShadow(b)
		}
	}
	s.byName = map[string]*passwdEntry{}
	for i := range s.passwd {
		if _, dup := s.byName[s.passwd[i].Name]; !dup {
			s.byName[s.passwd[i].Name] = &s.passwd[i]
		}
	}
	s.groupName = map[string]*groupEntry{}
	for i := range s.groups {
		if _, dup := s.groupName[s.groups[i].Name]; !dup {
			s.groupName[s.groups[i].Name] = &s.groups[i]
		}
	}
	return s, nil
}

// isPerson reports whether the account is a human one: UID >= UID_MIN (below
// the "nobody" range) or root.
func (s *system) isPerson(p *passwdEntry) bool {
	return p.UID == 0 || (p.UID >= s.uidMin && p.UID < 65534)
}

// supplementary returns the groups the user is listed in (primary excluded).
func (s *system) supplementary(name string) []string {
	out := []string{}
	for _, g := range s.groups {
		for _, m := range g.Members {
			if m == name {
				out = append(out, g.Name)
				break
			}
		}
	}
	return out
}

func (s *system) groupByGID(gid int) *groupEntry {
	for i := range s.groups {
		if s.groups[i].GID == gid {
			return &s.groups[i]
		}
	}
	return nil
}

// adminGroup returns the group used to grant administrator rights.
func (s *system) adminGroup() string {
	for _, n := range adminGroups {
		if _, ok := s.groupName[n]; ok {
			return n
		}
	}
	return ""
}

func (s *system) isAdmin(p *passwdEntry) bool {
	if p.UID == 0 {
		return true
	}
	for _, n := range adminGroups {
		g := s.groupName[n]
		if g == nil {
			continue
		}
		if g.GID == p.GID {
			return true
		}
		for _, m := range g.Members {
			if m == p.Name {
				return true
			}
		}
	}
	return false
}

func (s *system) shellAllowed(sh string) bool {
	for _, x := range s.shells {
		if x == sh {
			return true
		}
	}
	return false
}
