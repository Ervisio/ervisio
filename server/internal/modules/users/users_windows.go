//go:build windows

package users

// Windows backend: local accounts are read and changed through PowerShell's
// Microsoft.PowerShell.LocalAccounts cmdlets. Scripts are fixed text sent as
// -EncodedCommand; every piece of user data (names, passwords...) travels as
// ASCII-escaped JSON on stdin, so nothing is interpolated into a script or
// put on a command line.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func registerPlatform(r *rpc.Registry) bool {
	r.Handle("users.list", rpc.User, winList)
	r.Handle("users.groups", rpc.User, winGroups)
	for _, m := range []string{"users.sessions", "users.sshKeys", "users.addSshKey", "users.removeSshKey", "users.terminateSession"} {
		role := rpc.User
		if m == "users.terminateSession" {
			role = rpc.Admin
		}
		what := map[string]string{
			"users.sessions": "Listing sessions", "users.sshKeys": "SSH authorized keys",
			"users.addSshKey": "SSH authorized keys", "users.removeSshKey": "SSH authorized keys",
			"users.terminateSession": "Ending sessions",
		}[m]
		r.Handle(m, role, func(context.Context, *rpc.Call) (any, error) { return nil, winUnavailable(what) })
	}
	r.Handle("users.create", rpc.Admin, winCreate)
	r.Handle("users.modify", rpc.Admin, winModify)
	r.Handle("users.setPassword", rpc.Admin, winSetPassword)
	r.Handle("users.lock", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return winLock(ctx, c) })
	r.Handle("users.unlock", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return winUnlock(ctx, c) })
	r.Handle("users.delete", rpc.Admin, winDelete)
	r.Handle("users.setAdmin", rpc.Admin, winSetAdmin)
	r.Handle("groups.create", rpc.Admin, winGroupCreate)
	r.Handle("groups.delete", rpc.Admin, winGroupDelete)
	r.Handle("groups.addMember", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return winGroupMember(ctx, c, true) })
	r.Handle("groups.removeMember", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) { return winGroupMember(ctx, c, false) })
	return true
}

const psPrelude = `$ErrorActionPreference='Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
try {
$raw = [Console]::In.ReadToEnd()
$in = $null
if ($raw.Trim()) { $in = $raw | ConvertFrom-Json }
`

const psPostlude = `
} catch {
[Console]::Error.WriteLine('ERR|' + $_.FullyQualifiedErrorId + '|' + $_.Exception.GetType().FullName + '|' + ($_.Exception.Message -replace '\s+',' '))
exit 1
}
`

func powershellPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "powershell.exe"
}

func encodeScript(body string) string {
	u := utf16.Encode([]rune(psPrelude + body + psPostlude))
	b := make([]byte, 0, len(u)*2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(b)
}

// runPS runs a script with input as JSON on stdin and returns its stdout.
func runPS(ctx context.Context, what, body string, input any) ([]byte, error) {
	var stdin []byte
	if input != nil {
		var err error
		if stdin, err = asciiJSON(input); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, powershellPath(), "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeScript(body))
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "%s: PowerShell timed out", what)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			msg := strings.TrimSpace(errb.String())
			if msg == "" {
				msg = "PowerShell exited with " + ee.ProcessState.String()
			}
			return nil, classifyPSError(what, msg)
		}
		return nil, rpc.Errorf(rpc.Unavailable, "%s: PowerShell could not be started: %v", what, err)
	}
	return out.Bytes(), nil
}

const psSnapshot = `
$prof = @{}
try { Get-CimInstance Win32_UserProfile | ForEach-Object { $prof[$_.SID] = $_.LocalPath } } catch {}
function U($d) { if ($d) { [DateTimeOffset]::new($d.ToUniversalTime()).ToUnixTimeSeconds() } else { 0 } }
$me = [Security.Principal.WindowsIdentity]::GetCurrent()
$users = @(Get-LocalUser | ForEach-Object {
  [pscustomobject]@{ name=$_.Name; sid=$_.SID.Value; fullName=[string]$_.FullName; description=[string]$_.Description
    enabled=[bool]$_.Enabled; passwordRequired=[bool]$_.PasswordRequired; passwordLastSet=(U $_.PasswordLastSet)
    lastLogon=(U $_.LastLogon); accountExpires=(U $_.AccountExpires); profile=[string]$prof[$_.SID.Value] } })
$groups = @(Get-LocalGroup | ForEach-Object {
  $g = $_; $m = @()
  try { $m = @(Get-LocalGroupMember -Group $g.Name -ErrorAction Stop | ForEach-Object { [pscustomobject]@{ name=$_.Name; sid=$_.SID.Value } }) } catch {}
  [pscustomobject]@{ name=$g.Name; sid=$g.SID.Value; description=[string]$g.Description; members=$m } })
[pscustomobject]@{ computer=$env:COMPUTERNAME; self=$me.User.Value; selfName=($me.Name -replace '^.*\\',''); users=$users; groups=$groups } | ConvertTo-Json -Depth 5 -Compress
`

// snapshot reads accounts natively (works for unprivileged and profile-less
// tokens) and only falls back to PowerShell if the native read fails.
func snapshot(ctx context.Context) (*winSnapshot, error) {
	s, nerr := nativeSnapshot(time.Now())
	if nerr == nil {
		return s, nil
	}
	s, err := psSnapshotRead(ctx)
	if err != nil {
		if rpc.IsCode(nerr, rpc.NeedsAdmin) {
			return nil, nerr
		}
		return nil, err
	}
	return s, nil
}

func psSnapshotRead(ctx context.Context) (*winSnapshot, error) {
	out, err := runPS(ctx, "Could not read the accounts", psSnapshot, nil)
	if err != nil {
		return nil, err
	}
	return parseWinSnapshot(out)
}

var winMu sync.Mutex // serialises our own account changes

func winList(ctx context.Context, c *rpc.Call) (any, error) {
	s, err := snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.listResult(time.Now()), nil
}

func winGroups(ctx context.Context, c *rpc.Call) (any, error) {
	s, err := snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return s.groupsResult(), nil
}

// winLockSnapshot takes winMu and loads the snapshot. On success the caller
// must call unlock.
func winLockSnapshot(ctx context.Context) (*winSnapshot, func(), error) {
	winMu.Lock()
	s, err := snapshot(ctx)
	if err != nil {
		winMu.Unlock()
		return nil, nil, err
	}
	return s, winMu.Unlock, nil
}

// winLockTarget takes winMu and finds the named person (see winTarget). On
// success the caller must call unlock.
func winLockTarget(ctx context.Context, name string) (*winSnapshot, *winUser, func(), error) {
	winMu.Lock()
	s, u, err := winTarget(ctx, name, true)
	if err != nil {
		winMu.Unlock()
		return nil, nil, nil, err
	}
	return s, u, winMu.Unlock, nil
}

// winTarget loads the snapshot and finds a person account.
func winTarget(ctx context.Context, name string, allowBuiltin bool) (*winSnapshot, *winUser, error) {
	if err := validWinUser(name); err != nil {
		return nil, nil, err
	}
	s, err := snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	u := s.user(name)
	if u == nil {
		return nil, nil, rpc.Errorf(rpc.NotFound, "There is no user called %s", name)
	}
	if !s.isPerson(*u) || (!allowBuiltin && ridOf(u.SID) == 500) {
		return nil, nil, rpc.Errorf(rpc.Forbidden, "%s is a system account; it cannot be changed here", u.Name)
	}
	return s, u, nil
}

func resolveGroups(s *winSnapshot, names []string) ([]string, error) {
	out := []string{}
	for _, n := range uniq(names) {
		if err := validWinGroup(n); err != nil {
			return nil, err
		}
		g := s.group(n)
		if g == nil {
			return nil, rpc.Errorf(rpc.NotFound, "There is no group called %s", n)
		}
		out = append(out, g.Name)
	}
	return out, nil
}

func adminGroupName(s *winSnapshot) string {
	for _, g := range s.Groups {
		if g.SID == sidAdministrators {
			return g.Name
		}
	}
	return ""
}

const psCreate = `
$p = @{ Name = $in.name }
if ($in.fullName) { $p.FullName = $in.fullName }
if ($in.password) { $p.Password = ConvertTo-SecureString $in.password -AsPlainText -Force } else { $p.NoPassword = $true }
New-LocalUser @p | Out-Null
try { foreach ($g in @($in.groups)) { Add-LocalGroupMember -Group $g -Member $in.name } }
catch { Remove-LocalUser -Name $in.name -ErrorAction SilentlyContinue; throw }
`

func winCreate(ctx context.Context, c *rpc.Call) (any, error) {
	var p createParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validWinCreateInput(p); err != nil {
		return nil, err
	}
	s, unlock, err := winLockSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if s.user(p.Name) != nil || s.group(p.Name) != nil {
		return nil, rpc.Errorf(rpc.Conflict, "A user or group called %s already exists", p.Name)
	}
	groups, err := winCreateGroups(s, p)
	if err != nil {
		return nil, err
	}
	in := map[string]any{"name": p.Name, "fullName": p.FullName, "password": p.Password, "groups": groups}
	if _, err := runPS(ctx, "Could not create the user", psCreate, in); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "name": p.Name}, nil
}

func validWinCreateInput(p createParams) error {
	if err := validWinUser(p.Name); err != nil {
		return err
	}
	if err := validWinText("full name", p.FullName, 128); err != nil {
		return err
	}
	if p.Password != "" {
		return validPassword(p.Password)
	}
	return nil
}

func winCreateGroups(s *winSnapshot, p createParams) ([]string, error) {
	groups, err := resolveGroups(s, p.Groups)
	if err != nil {
		return nil, err
	}
	if p.Admin {
		ag := adminGroupName(s)
		if ag == "" {
			return nil, rpc.Errorf(rpc.Conflict, "This system has no Administrators group")
		}
		groups = uniq(append(groups, ag))
	}
	return groups, nil
}

const psModify = `
$n = $in.name
if ($in.PSObject.Properties['fullName']) { Set-LocalUser -Name $n -FullName ([string]$in.fullName) }
if ($in.PSObject.Properties['description']) { Set-LocalUser -Name $n -Description ([string]$in.description) }
foreach ($g in @($in.add)) { Add-LocalGroupMember -Group $g -Member $n -ErrorAction SilentlyContinue }
foreach ($g in @($in.remove)) { Remove-LocalGroupMember -Group $g -Member $n -ErrorAction SilentlyContinue }
`

func winModify(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name        string  `json:"name"`
		FullName    *string `json:"fullName"`
		Description *string `json:"description"`
		Shell       *string `json:"shell"`
		Groups      *struct {
			Add    []string `json:"add"`
			Remove []string `json:"remove"`
		} `json:"groups"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if p.Shell != nil && *p.Shell != "" {
		return nil, winUnavailable("Changing the login shell")
	}
	in := map[string]any{"name": p.Name}
	if err := setWinText(in, "fullName", "full name", p.FullName, 128); err != nil {
		return nil, err
	}
	if err := setWinText(in, "description", "description", p.Description, 256); err != nil {
		return nil, err
	}
	s, u, unlock, err := winLockTarget(ctx, p.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	in["name"] = u.Name
	if p.Groups != nil {
		add, rem, err := winPlanGroups(s, u, p.Groups.Add, p.Groups.Remove)
		if err != nil {
			return nil, err
		}
		in["add"], in["remove"] = add, rem
	}
	if _, err := runPS(ctx, "Could not change the user", psModify, in); err != nil {
		return nil, err
	}
	return okResult(), nil
}

// setWinText validates an optional text field and stores it in in[key].
func setWinText(in map[string]any, key, label string, val *string, limit int) error {
	if val == nil {
		return nil
	}
	if err := validWinText(label, *val, limit); err != nil {
		return err
	}
	in[key] = *val
	return nil
}

// winPlanGroups resolves the group lists and refuses to drop the last administrator.
func winPlanGroups(s *winSnapshot, u *winUser, addIn, remIn []string) (add, rem []string, err error) {
	if add, err = resolveGroups(s, addIn); err != nil {
		return nil, nil, err
	}
	if rem, err = resolveGroups(s, remIn); err != nil {
		return nil, nil, err
	}
	for _, g := range rem {
		if gg := s.group(g); gg != nil && gg.SID == sidAdministrators && s.otherAdmins(u.Name) == 0 {
			return nil, nil, rpc.Errorf(rpc.Conflict, "%s is the only administrator. Make someone else an administrator first", u.Name)
		}
	}
	return add, rem, nil
}

const psSetPassword = `
Set-LocalUser -Name $in.name -Password (ConvertTo-SecureString $in.password -AsPlainText -Force)
if ($in.mustChange) { $u = [ADSI]("WinNT://./" + $in.name + ",user"); $u.PasswordExpired = 1; $u.SetInfo() }
`

func winSetPassword(ctx context.Context, c *rpc.Call) (any, error) {
	p, err := bindPassword(c)
	if err != nil {
		return nil, err
	}
	winMu.Lock()
	defer winMu.Unlock()
	_, u, err := winTarget(ctx, p.Name, true)
	if err != nil {
		return nil, err
	}
	in := map[string]any{"name": u.Name, "password": p.Password, "mustChange": p.MustChange}
	if _, err := runPS(ctx, "Could not set the password", psSetPassword, in); err != nil {
		return nil, err
	}
	return okResult(), nil
}

const psEnable = `if ($in.enable) { Enable-LocalUser -Name $in.name } else { Disable-LocalUser -Name $in.name }`

func winLock(ctx context.Context, c *rpc.Call) (any, error) {
	return winSetEnabled(ctx, c, false)
}

func winUnlock(ctx context.Context, c *rpc.Call) (any, error) {
	return winSetEnabled(ctx, c, true)
}

func winSetEnabled(ctx context.Context, c *rpc.Call, enable bool) (any, error) {
	var p nameParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	s, u, unlock, err := winLockTarget(ctx, p.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !enable && u.SID == s.Self {
		return nil, rpc.Errorf(rpc.Forbidden, "You cannot lock your own account")
	}
	if _, err := runPS(ctx, "Could not change the account state", psEnable, map[string]any{"name": u.Name, "enable": enable}); err != nil {
		return nil, err
	}
	return okResult(), nil
}

const psDelete = `
if ($in.removeHome) { Get-CimInstance Win32_UserProfile | Where-Object { $_.SID -eq $in.sid } | Remove-CimInstance }
Remove-LocalUser -SID $in.sid
`

func winDelete(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name       string `json:"name"`
		RemoveHome bool   `json:"removeHome"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	winMu.Lock()
	defer winMu.Unlock()
	s, u, err := winTarget(ctx, p.Name, false)
	if err != nil {
		return nil, err
	}
	if u.SID == s.Self {
		return nil, rpc.Errorf(rpc.Forbidden, "You cannot delete the account you are signed in with")
	}
	if _, err := runPS(ctx, "Could not delete the user", psDelete, map[string]any{"sid": u.SID, "removeHome": p.RemoveHome}); err != nil {
		return nil, err
	}
	return okResult(), nil
}

const psMember = `
if ($in.add) { Add-LocalGroupMember -Group $in.group -Member $in.user } else { Remove-LocalGroupMember -Group $in.group -Member $in.user }
`

func winMemberChange(ctx context.Context, s *winSnapshot, u *winUser, group string, add bool) error {
	groups, _ := s.groupsOf(*u)
	for _, g := range groups {
		if strings.EqualFold(g, group) == add {
			return nil // already in the wanted state
		}
	}
	what := "Could not add " + u.Name + " to " + group
	if !add {
		what = "Could not remove " + u.Name + " from " + group
	}
	_, err := runPS(ctx, what, psMember, map[string]any{"group": group, "user": u.Name, "add": add})
	return err
}

func winSetAdmin(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Name  string `json:"name"`
		Admin bool   `json:"admin"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	s, u, unlock, err := winLockTarget(ctx, p.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	ag := adminGroupName(s)
	if ag == "" {
		return nil, rpc.Errorf(rpc.Conflict, "This system has no Administrators group")
	}
	if ridOf(u.SID) == 500 && !p.Admin {
		return nil, rpc.Errorf(rpc.Forbidden, "The built-in Administrator always has administrator rights")
	}
	if !p.Admin && s.otherAdmins(u.Name) == 0 {
		return nil, rpc.Errorf(rpc.Conflict, "%s is the only administrator. Make someone else an administrator first", u.Name)
	}
	if err := winMemberChange(ctx, s, u, ag, p.Admin); err != nil {
		return nil, err
	}
	return okResult(), nil
}

const psGroupCreate = `New-LocalGroup -Name $in.name | Out-Null`
const psGroupDelete = `Remove-LocalGroup -SID $in.sid`

// bindWinGroup binds the group name of a call and checks it.
func bindWinGroup(c *rpc.Call) (nameParams, error) {
	var p nameParams
	if err := c.Bind(&p); err != nil {
		return p, err
	}
	if err := validWinGroup(p.Name); err != nil {
		return p, err
	}
	return p, nil
}

func winGroupCreate(ctx context.Context, c *rpc.Call) (any, error) {
	p, err := bindWinGroup(c)
	if err != nil {
		return nil, err
	}
	s, unlock, err := winLockSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if s.group(p.Name) != nil || s.user(p.Name) != nil {
		return nil, rpc.Errorf(rpc.Conflict, "A group or user called %s already exists", p.Name)
	}
	if _, err := runPS(ctx, "Could not create the group", psGroupCreate, map[string]any{"name": p.Name}); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "name": p.Name}, nil
}

func winGroupDelete(ctx context.Context, c *rpc.Call) (any, error) {
	p, err := bindWinGroup(c)
	if err != nil {
		return nil, err
	}
	s, unlock, err := winLockSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	g := s.group(p.Name)
	if g == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no group called %s", p.Name)
	}
	if isBuiltinGroupSID(g.SID) || ridOf(g.SID) < 1000 {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is a system group and cannot be deleted", g.Name)
	}
	if _, err := runPS(ctx, "Could not delete the group", psGroupDelete, map[string]any{"sid": g.SID}); err != nil {
		return nil, err
	}
	return okResult(), nil
}

func winGroupMember(ctx context.Context, c *rpc.Call, add bool) (any, error) {
	var p struct {
		Group string `json:"group"`
		User  string `json:"user"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validWinGroup(p.Group); err != nil {
		return nil, err
	}
	winMu.Lock()
	defer winMu.Unlock()
	s, u, err := winTarget(ctx, p.User, true)
	if err != nil {
		return nil, err
	}
	g := s.group(p.Group)
	if g == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no group called %s", p.Group)
	}
	if !add && g.SID == sidAdministrators && s.otherAdmins(u.Name) == 0 {
		return nil, rpc.Errorf(rpc.Conflict, "%s is the only administrator. Make someone else an administrator first", u.Name)
	}
	if err := winMemberChange(ctx, s, u, g.Name, add); err != nil {
		return nil, err
	}
	return okResult(), nil
}
