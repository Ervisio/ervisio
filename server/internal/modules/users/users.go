// Package users implements the bridge methods of the Users section (users.*,
// groups.*). See docs/api/users.md.
//
// Reads parse /etc/passwd and /etc/group directly; changes go through the
// shadow-utils tools (useradd, usermod, userdel, gpasswd, chpasswd...) as argv,
// with passwords only ever on stdin.
package users

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// Register adds the users.* and groups.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("users.list", rpc.User, list)
	r.Handle("users.groups", rpc.User, groups)
	r.Handle("users.sessions", rpc.User, sessions)
	r.Handle("users.sshKeys", rpc.User, sshKeys)
	r.Handle("users.addSshKey", rpc.User, addSSHKey)
	r.Handle("users.removeSshKey", rpc.User, removeSSHKey)

	r.Handle("users.create", rpc.Admin, create)
	r.Handle("users.modify", rpc.Admin, modify)
	r.Handle("users.setPassword", rpc.Admin, setPassword)
	r.Handle("users.lock", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return lock(ctx, c, true) })
	r.Handle("users.unlock", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return lock(ctx, c, false) })
	r.Handle("users.delete", rpc.Admin, deleteUser)
	r.Handle("users.setAdmin", rpc.Admin, setAdmin)
	r.Handle("users.terminateSession", rpc.Admin, terminateSession)

	r.Handle("groups.create", rpc.Admin, groupCreate)
	r.Handle("groups.delete", rpc.Admin, groupDelete)
	r.Handle("groups.addMember", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return groupMember(ctx, c, true) })
	r.Handle("groups.removeMember", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return groupMember(ctx, c, false) })
}

// ---- helpers ----

// callerUID is the user on whose behalf we run: the sudo invoker in the root
// bridge, the process user otherwise.
func callerUID() int {
	if os.Geteuid() == 0 {
		if s := os.Getenv("SUDO_UID"); s != "" {
			return atoi(s)
		}
	}
	return os.Getuid()
}

func okResult() map[string]any { return map[string]any{"ok": true} }

// runTool runs a shadow-utils command and turns failures into readable errors.
func runTool(ctx context.Context, what, stdin string, name string, args ...string) error {
	cmd := sys.Cmd{Name: name, Args: args}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	err := cmd.Run(ctx)
	if err == nil {
		return nil
	}
	var ee *sys.ExitError
	if errors.As(err, &ee) {
		msg := strings.TrimSpace(ee.Stderr)
		if i := strings.LastIndex(msg, "\n"); i >= 0 && strings.Count(msg, "\n") > 3 {
			msg = strings.TrimSpace(msg[i:])
		}
		code := rpc.Internal
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "in use") || strings.Contains(msg, "currently used") {
			code = rpc.Conflict
		}
		if msg == "" {
			msg = name + " failed"
		}
		return rpc.Errorf(code, "%s: %s", what, msg)
	}
	return err
}

var dbMu sync.Mutex // serialises our own account changes

func person(s *system, name string, allowRoot bool) (*passwdEntry, error) {
	if err := validName("User", name); err != nil {
		return nil, err
	}
	p := s.byName[name]
	if p == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no user called %s", name)
	}
	if !s.isPerson(p) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is a system account; it cannot be changed here", name)
	}
	if p.UID == 0 && !allowRoot {
		return nil, rpc.Errorf(rpc.Forbidden, "The root account cannot be changed this way")
	}
	return p, nil
}

func noLoginShell(sh string) bool {
	return sh == "" || strings.HasSuffix(sh, "/nologin") || strings.HasSuffix(sh, "/false")
}

// ---- users.list ----

type userInfo struct {
	Name            string     `json:"name"`
	UID             int        `json:"uid"`
	GID             int        `json:"gid"`
	PrimaryGroup    string     `json:"primaryGroup"`
	FullName        string     `json:"fullName"`
	Home            string     `json:"home"`
	Shell           string     `json:"shell"`
	Groups          []string   `json:"groups"`
	IsAdmin         bool       `json:"isAdmin"`
	Locked          *bool      `json:"locked"`          // null when /etc/shadow is not readable
	PasswordState   string     `json:"passwordState"`   // ok, locked, disabled, empty, unknown
	PasswordChanged *int       `json:"passwordChanged"` // days ago
	MustChange      bool       `json:"mustChange"`
	Expired         bool       `json:"expired"`
	NoLoginShell    bool       `json:"noLoginShell"`
	LastLogin       *loginInfo `json:"lastLogin"`
	NeverLoggedIn   bool       `json:"neverLoggedIn"`
}

func (s *system) info(p *passwdEntry, logins map[string]loginInfo, never map[string]bool) userInfo {
	u := userInfo{
		Name: p.Name, UID: p.UID, GID: p.GID, FullName: strings.TrimSpace(strings.SplitN(p.GECOS, ",", 2)[0]),
		Home: p.Home, Shell: p.Shell, Groups: s.supplementary(p.Name), IsAdmin: s.isAdmin(p),
		PasswordState: "unknown", NoLoginShell: noLoginShell(p.Shell),
	}
	if g := s.groupByGID(p.GID); g != nil {
		u.PrimaryGroup = g.Name
	}
	if s.shadow != nil {
		if e, ok := s.shadow[p.Name]; ok {
			u.PasswordState = passwordState(e.Hash)
			l := u.PasswordState == "locked"
			u.Locked = &l
			today := int(time.Now().Unix() / 86400)
			switch {
			case e.LastChange == 0:
				u.MustChange = true
			case e.LastChange > 0:
				d := today - e.LastChange
				if d < 0 {
					d = 0
				}
				u.PasswordChanged = &d
			}
			u.Expired = e.Expire >= 0 && e.Expire < today
		}
	}
	if li, ok := logins[p.Name]; ok {
		l := li
		u.LastLogin = &l
	} else if never[p.Name] {
		u.NeverLoggedIn = true
	}
	return u
}

func list(ctx context.Context, c *rpc.Call) (any, error) {
	s, err := loadSystem(os.Geteuid() == 0)
	if err != nil {
		return nil, err
	}
	logins, never, known := lastLogins(ctx)
	if !known {
		never = map[string]bool{}
	}
	people, system := []userInfo{}, []userInfo{}
	for i := range s.passwd {
		u := s.info(&s.passwd[i], logins, never)
		if s.isPerson(&s.passwd[i]) {
			people = append(people, u)
		} else {
			system = append(system, u)
		}
	}
	sort.SliceStable(people, func(i, j int) bool { return people[i].UID < people[j].UID })
	sort.SliceStable(system, func(i, j int) bool { return system[i].UID < system[j].UID })
	self := ""
	for _, p := range s.passwd {
		if p.UID == callerUID() {
			self = p.Name
			break
		}
	}
	shells := s.shells
	if shells == nil {
		shells = []string{}
	}
	return map[string]any{
		"people": people, "system": system, "uidMin": s.uidMin,
		"shadowReadable": s.shadow != nil, "lastLoginKnown": known,
		"adminGroup": s.adminGroup(), "shells": shells, "self": self,
	}, nil
}

// ---- users.groups ----

var groupDescriptions = map[string]string{
	"wheel": "Can use sudo", "sudo": "Can use sudo", "admin": "Can use sudo",
	"docker": "Manage containers", "podman": "Manage containers", "libvirt": "Manage virtual machines",
	"kvm": "Run virtual machines", "audio": "Play and record sound", "video": "Use cameras and graphics",
	"input": "Read input devices", "storage": "Mount removable drives", "network": "Manage network connections",
	"networkmanager": "Manage network connections", "lp": "Use printers", "scanner": "Use scanners",
	"render": "Use the GPU", "optical": "Use CD and DVD drives", "disk": "Raw access to disks",
	"adm": "Read system logs", "systemd-journal": "Read the system journal", "uucp": "Use serial ports",
	"dialout": "Use serial ports", "tty": "Access terminals", "users": "Regular users", "power": "Power management",
	"bluetooth": "Use Bluetooth", "plugdev": "Use removable devices", "cdrom": "Use CD drives",
	"wireshark": "Capture network traffic", "sambashare": "Share folders over the network",
	"vboxusers": "Use VirtualBox", "root": "Super user", "nobody": "Unprivileged", "nogroup": "Unprivileged",
	"floppy": "Use floppy drives", "games": "Game scores", "http": "Web server files", "ftp": "FTP server files",
}

type groupInfo struct {
	Name           string   `json:"name"`
	GID            int      `json:"gid"`
	Members        []string `json:"members"`
	PrimaryMembers []string `json:"primaryMembers"`
	System         bool     `json:"system"`
	Description    string   `json:"description"`
}

func groups(ctx context.Context, c *rpc.Call) (any, error) {
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	out := []groupInfo{}
	for _, g := range s.groups {
		gi := groupInfo{Name: g.Name, GID: g.GID, Members: g.Members, PrimaryMembers: []string{},
			System: g.GID < s.gidMin || g.GID >= 65534, Description: groupDescriptions[g.Name]}
		for _, p := range s.passwd {
			if p.GID == g.GID {
				gi.PrimaryMembers = append(gi.PrimaryMembers, p.Name)
			}
		}
		out = append(out, gi)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GID < out[j].GID })
	return out, nil
}

// ---- users.create ----

type createParams struct {
	Name       string   `json:"name"`
	FullName   string   `json:"fullName"`
	Shell      string   `json:"shell"`
	Password   string   `json:"password"`
	Admin      bool     `json:"admin"`
	Groups     []string `json:"groups"`
	CreateHome *bool    `json:"createHome"`
}

func buildUseradd(p createParams, groups []string, createHome bool) []string {
	args := []string{"-c", p.FullName, "-s", p.Shell}
	if createHome {
		args = append(args, "-m")
	} else {
		args = append(args, "-M")
	}
	if len(groups) > 0 {
		args = append(args, "-G", strings.Join(groups, ","))
	}
	return append(args, p.Name)
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range in {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func checkGroups(s *system, names []string) error {
	for _, g := range names {
		if err := validName("Group", g); err != nil {
			return err
		}
		if s.groupName[g] == nil {
			return rpc.Errorf(rpc.NotFound, "There is no group called %s", g)
		}
	}
	return nil
}

func create(ctx context.Context, c *rpc.Call) (any, error) {
	var p createParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validName("User", p.Name); err != nil {
		return nil, err
	}
	if err := validFullName(p.FullName); err != nil {
		return nil, err
	}
	if p.Password != "" {
		if err := validPassword(p.Password); err != nil {
			return nil, err
		}
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	if s.byName[p.Name] != nil {
		return nil, rpc.Errorf(rpc.Conflict, "A user called %s already exists", p.Name)
	}
	if s.groupName[p.Name] != nil {
		return nil, rpc.Errorf(rpc.Conflict, "A group called %s already exists; choose another user name", p.Name)
	}
	if p.Shell == "" {
		p.Shell = "/bin/bash"
		if !s.shellAllowed(p.Shell) && len(s.shells) > 0 {
			p.Shell = s.shells[0]
		}
	}
	if !s.shellAllowed(p.Shell) {
		return nil, rpc.Errorf(rpc.Invalid, "%s is not a valid login shell (see /etc/shells)", p.Shell)
	}
	groups := uniq(p.Groups)
	if p.Admin {
		ag := s.adminGroup()
		if ag == "" {
			return nil, rpc.Errorf(rpc.Conflict, "This system has no wheel, sudo or admin group to grant administrator rights")
		}
		groups = uniq(append(groups, ag))
	}
	if err := checkGroups(s, groups); err != nil {
		return nil, err
	}
	home := p.CreateHome == nil || *p.CreateHome
	if err := runTool(ctx, "Could not create the user", "", "useradd", buildUseradd(p, groups, home)...); err != nil {
		return nil, err
	}
	if p.Password != "" {
		if err := runTool(ctx, "The user was created but the password was rejected", p.Name+":"+p.Password+"\n", "chpasswd"); err != nil {
			rb := []string{p.Name}
			if home {
				rb = []string{"-r", p.Name}
			}
			_ = runTool(ctx, "rollback", "", "userdel", rb...)
			return nil, rpc.Errorf(rpc.Invalid, "The password was rejected, so the user was not created: %s", strings.TrimPrefix(err.Error(), "invalid: "))
		}
	}
	return map[string]any{"ok": true, "name": p.Name}, nil
}

// ---- users.modify ----

type modifyParams struct {
	Name     string  `json:"name"`
	FullName *string `json:"fullName"`
	Shell    *string `json:"shell"`
	Groups   *struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	} `json:"groups"`
}

func modify(ctx context.Context, c *rpc.Call) (any, error) {
	var p modifyParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	u, err := person(s, p.Name, true)
	if err != nil {
		return nil, err
	}
	var add, rem []string
	if p.Groups != nil {
		add, rem = uniq(p.Groups.Add), uniq(p.Groups.Remove)
		if err := checkGroups(s, append(append([]string{}, add...), rem...)); err != nil {
			return nil, err
		}
		for _, g := range rem {
			if s.groupName[g].GID == u.GID {
				return nil, rpc.Errorf(rpc.Invalid, "%s is the primary group of %s and cannot be removed", g, u.Name)
			}
		}
	}
	if p.FullName != nil {
		if err := validFullName(*p.FullName); err != nil {
			return nil, err
		}
		if err := runTool(ctx, "Could not change the full name", "", "usermod", "-c", *p.FullName, u.Name); err != nil {
			return nil, err
		}
	}
	if p.Shell != nil && *p.Shell != u.Shell {
		if !s.shellAllowed(*p.Shell) {
			return nil, rpc.Errorf(rpc.Invalid, "%s is not a valid login shell (see /etc/shells)", *p.Shell)
		}
		if err := runTool(ctx, "Could not change the shell", "", "usermod", "-s", *p.Shell, u.Name); err != nil {
			return nil, err
		}
	}
	for _, g := range add {
		if err := memberChange(ctx, s, u.Name, g, true); err != nil {
			return nil, err
		}
	}
	for _, g := range rem {
		if err := memberChange(ctx, s, u.Name, g, false); err != nil {
			return nil, err
		}
	}
	return okResult(), nil
}

func isMember(s *system, user, group string) bool {
	g := s.groupName[group]
	if g == nil {
		return false
	}
	for _, m := range g.Members {
		if m == user {
			return true
		}
	}
	return false
}

// memberChange adds or removes user to/from group (idempotent).
func memberChange(ctx context.Context, s *system, user, group string, add bool) error {
	if isMember(s, user, group) == add {
		return nil
	}
	if add {
		return runTool(ctx, "Could not add "+user+" to "+group, "", "gpasswd", "-a", user, group)
	}
	return runTool(ctx, "Could not remove "+user+" from "+group, "", "gpasswd", "-d", user, group)
}

// ---- users.setPassword / lock / delete / setAdmin ----

func setPassword(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name       string `json:"name"`
		Password   string `json:"password"`
		MustChange bool   `json:"mustChange"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validPassword(p.Password); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(true)
	if err != nil {
		return nil, err
	}
	u, err := person(s, p.Name, true)
	if err != nil {
		return nil, err
	}
	wasLocked := false
	if e, ok := s.shadow[u.Name]; ok {
		wasLocked = passwordState(e.Hash) == "locked"
	}
	if err := runTool(ctx, "Could not set the password", u.Name+":"+p.Password+"\n", "chpasswd"); err != nil {
		return nil, err
	}
	if wasLocked { // chpasswd drops the lock; keep the account locked
		if err := runTool(ctx, "Could not keep the account locked", "", "usermod", "-L", u.Name); err != nil {
			return nil, err
		}
	}
	if p.MustChange {
		if err := runTool(ctx, "Could not require a password change", "", "chage", "-d", "0", u.Name); err != nil {
			return nil, err
		}
	}
	return okResult(), nil
}

func lock(ctx context.Context, c *rpc.Call, lock bool) (any, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	u, err := person(s, p.Name, true)
	if err != nil {
		return nil, err
	}
	if lock {
		if u.UID == callerUID() {
			return nil, rpc.Errorf(rpc.Forbidden, "You cannot lock your own account")
		}
		return okResult(), runTool(ctx, "Could not lock the account", "", "usermod", "-L", u.Name)
	}
	return okResult(), runTool(ctx, "Could not unlock the account (set a password first if it has none)", "", "usermod", "-U", u.Name)
}

func deleteUser(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name       string `json:"name"`
		RemoveHome bool   `json:"removeHome"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	u, err := person(s, p.Name, true)
	if err != nil {
		return nil, err
	}
	if u.UID == 0 {
		return nil, rpc.Errorf(rpc.Forbidden, "The root account cannot be deleted")
	}
	if u.UID == callerUID() {
		return nil, rpc.Errorf(rpc.Forbidden, "You cannot delete the account you are signed in with")
	}
	args := []string{u.Name}
	if p.RemoveHome {
		args = []string{"-r", u.Name}
	}
	if err := runTool(ctx, "Could not delete the user", "", "userdel", args...); err != nil {
		return nil, err
	}
	return okResult(), nil
}

func setAdmin(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name  string `json:"name"`
		Admin bool   `json:"admin"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	u, err := person(s, p.Name, true)
	if err != nil {
		return nil, err
	}
	if u.UID == 0 {
		return nil, rpc.Errorf(rpc.Forbidden, "root always has administrator rights")
	}
	if p.Admin {
		ag := s.adminGroup()
		if ag == "" {
			return nil, rpc.Errorf(rpc.Conflict, "This system has no wheel, sudo or admin group to grant administrator rights")
		}
		return okResult(), memberChange(ctx, s, u.Name, ag, true)
	}
	others := 0
	for i := range s.passwd {
		q := &s.passwd[i]
		if q.UID != 0 && q.Name != u.Name && s.isPerson(q) && s.isAdmin(q) {
			others++
		}
	}
	if others == 0 {
		return nil, rpc.Errorf(rpc.Conflict, "%s is the only administrator. Make someone else an administrator first", u.Name)
	}
	for _, ag := range adminGroups {
		if g := s.groupName[ag]; g != nil && g.GID == u.GID {
			return nil, rpc.Errorf(rpc.Conflict, "%s is the primary group of %s, so it cannot be removed", ag, u.Name)
		}
		if err := memberChange(ctx, s, u.Name, ag, false); err != nil {
			return nil, err
		}
	}
	return okResult(), nil
}

// ---- groups.* ----

func groupCreate(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validName("Group", p.Name); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	if s.groupName[p.Name] != nil {
		return nil, rpc.Errorf(rpc.Conflict, "A group called %s already exists", p.Name)
	}
	if err := runTool(ctx, "Could not create the group", "", "groupadd", p.Name); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "name": p.Name}, nil
}

func groupDelete(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validName("Group", p.Name); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	g := s.groupName[p.Name]
	if g == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no group called %s", p.Name)
	}
	if g.GID < s.gidMin || g.GID >= 65534 {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is a system group and cannot be deleted", p.Name)
	}
	for _, u := range s.passwd {
		if u.GID == g.GID {
			return nil, rpc.Errorf(rpc.Conflict, "%s is the primary group of %s; delete or change that user first", p.Name, u.Name)
		}
	}
	return okResult(), runTool(ctx, "Could not delete the group", "", "groupdel", p.Name)
}

func groupMember(ctx context.Context, c *rpc.Call, add bool) (any, error) {
	var p struct {
		Group string `json:"group"`
		User  string `json:"user"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validName("Group", p.Group); err != nil {
		return nil, err
	}
	if err := validName("User", p.User); err != nil {
		return nil, err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	s, err := loadSystem(false)
	if err != nil {
		return nil, err
	}
	g := s.groupName[p.Group]
	if g == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no group called %s", p.Group)
	}
	u := s.byName[p.User]
	if u == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no user called %s", p.User)
	}
	if g.GID == 0 {
		return nil, rpc.Errorf(rpc.Forbidden, "The root group cannot be changed here")
	}
	if !add && g.GID == u.GID {
		return nil, rpc.Errorf(rpc.Invalid, "%s is the primary group of %s and cannot be removed", p.Group, p.User)
	}
	if !s.isPerson(u) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is a system account; it cannot be changed here", p.User)
	}
	return okResult(), memberChange(ctx, s, p.User, p.Group, add)
}

// ---- SSH keys ----

var keysMu sync.Mutex

// keyTarget resolves the account and its authorized_keys path; other people's
// keys need the root bridge.
func keyTarget(c *rpc.Call, name string) (*passwdEntry, string, error) {
	s, err := loadSystem(false)
	if err != nil {
		return nil, "", err
	}
	if err := validName("User", name); err != nil {
		return nil, "", err
	}
	p := s.byName[name]
	if p == nil {
		return nil, "", rpc.Errorf(rpc.NotFound, "There is no user called %s", name)
	}
	if p.UID != callerUID() && !c.Admin {
		return nil, "", rpc.Errorf(rpc.NeedsAdmin, "Administrator rights are needed to manage the keys of %s", name)
	}
	if !filepath.IsAbs(p.Home) || p.Home == "/" {
		return nil, "", rpc.Errorf(rpc.Invalid, "%s has no home folder, so it has no SSH keys", name)
	}
	return p, filepath.Join(p.Home, ".ssh", "authorized_keys"), nil
}

func sshKeys(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	u, path, err := keyTarget(c, p.Name)
	if err != nil {
		return nil, err
	}
	var data string
	err = asUser(u.UID, u.GID, func() (e error) { data, e = readKeyFile(path); return })
	if err != nil {
		return nil, err
	}
	return parseAuthorizedKeys(data), nil
}

func addSSHKey(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	line := strings.TrimSpace(p.Key)
	if strings.ContainsAny(line, "\r\n") {
		return nil, rpc.Errorf(rpc.Invalid, "Paste one public key at a time")
	}
	k, ok, perr := parseKeyLine(line)
	if !ok {
		return nil, rpc.Errorf(rpc.Invalid, "Paste a public key, for example \"ssh-ed25519 AAAA… name\"")
	}
	if perr != nil {
		return nil, rpc.Errorf(rpc.Invalid, "That is not a valid public key: %s", perr)
	}
	u, path, err := keyTarget(c, p.Name)
	if err != nil {
		return nil, err
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	err = asUser(u.UID, u.GID, func() error {
		data, err := readKeyFile(path)
		if err != nil {
			return err
		}
		for _, e := range parseAuthorizedKeys(data) {
			if e.Fingerprint == k.Fingerprint {
				return rpc.Errorf(rpc.Conflict, "This key is already authorized")
			}
		}
		return writeKeyFile(path, appendKey(data, line))
	})
	if err != nil {
		return nil, err
	}
	return k, nil
}

func removeSSHKey(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name        string `json:"name"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(p.Fingerprint, "SHA256:") {
		return nil, rpc.Errorf(rpc.Invalid, "Give the SHA256 fingerprint of the key to remove")
	}
	u, path, err := keyTarget(c, p.Name)
	if err != nil {
		return nil, err
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	err = asUser(u.UID, u.GID, func() error {
		data, err := readKeyFile(path)
		if err != nil {
			return err
		}
		out, n := removeKey(data, p.Fingerprint)
		if n == 0 {
			return rpc.Errorf(rpc.NotFound, "That key is no longer in the file")
		}
		return writeKeyFile(path, out)
	})
	if err != nil {
		return nil, err
	}
	return okResult(), nil
}

// ---- sessions ----

func sessions(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if p.Name != "" {
		if err := validName("User", p.Name); err != nil {
			return nil, err
		}
	}
	return listSessions(ctx, p.Name)
}

func terminateSession(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if !sessionIDRe.MatchString(p.ID) {
		return nil, rpc.Errorf(rpc.Invalid, "Invalid session id")
	}
	return okResult(), runTool(ctx, "Could not end the session", "", "loginctl", "terminate-session", p.ID)
}
