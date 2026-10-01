// Package terminal implements the bridge methods of the Terminal section (terminal.*).
//
// Sessions are real ptys owned by the bridge process, so they survive the
// browser closing and are replayed (last 256 KB) when a client attaches again.
// See docs/api/terminal.md.
package terminal

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

var mgr = newManager()

// Register adds the terminal.* methods to the registry.
func Register(r *rpc.Registry) {
	r.Handle("terminal.create", rpc.User, handleCreate)
	r.Handle("terminal.list", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return map[string]any{"sessions": mgr.List()}, nil
	})
	r.Handle("terminal.rename", rpc.User, handleRename)
	r.Handle("terminal.kill", rpc.User, handleKill)
	r.Stream("terminal.attach", rpc.User, handleAttach)
}

type createParams struct {
	Kind  string `json:"kind"`
	Shell string `json:"shell"`
	Cwd   string `json:"cwd"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Name  string `json:"name"`
	Host  string `json:"host"`
	User  string `json:"user"`
	Port  int    `json:"port"`
}

func handleCreate(ctx context.Context, c *rpc.Call) (any, error) {
	var p createParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	sp, err := buildSpec(p, c.Admin)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		sp.Name = mgr.uniqueName(sp.Name)
	}
	s, err := mgr.Create(sp)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": s.id}, nil
}

func handleRename(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	name, err := cleanName(p.Name)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, rpc.Errorf(rpc.Invalid, "Give the session a name.")
	}
	s, err := mgr.Get(p.ID)
	if err != nil {
		return nil, err
	}
	s.Rename(name)
	return map[string]any{}, nil
}

func handleKill(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	s, err := mgr.Get(p.ID)
	if err != nil {
		return nil, err
	}
	s.Kill(2 * time.Second)
	return map[string]any{}, nil
}

type inputFrame struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func handleAttach(ctx context.Context, c *rpc.Call, st rpc.Stream) error {
	var p struct {
		ID   string `json:"id"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	s, err := mgr.Get(p.ID)
	if err != nil {
		return err
	}
	if p.Cols > 0 && p.Rows > 0 {
		if cols, rows, err := checkSize(p.Cols, p.Rows); err == nil {
			_ = s.Resize(cols, rows)
		}
	}
	replay, sub := s.Subscribe()
	defer s.Unsubscribe(sub)
	if len(replay) > 0 {
		if err := st.SendBytes(replay); err != nil {
			return nil
		}
	}
	in := st.Input()
	for {
		select {
		case <-ctx.Done():
			return nil
		case b := <-sub.ch:
			if err := st.SendBytes(b); err != nil {
				return nil
			}
		case <-sub.dropped:
			return rpc.Errorf(rpc.Unavailable, "The browser fell behind the terminal output. Reconnecting.")
		case <-s.Done():
			for {
				select {
				case b := <-sub.ch:
					if err := st.SendBytes(b); err != nil {
						return nil
					}
				default:
					_ = st.Send(map[string]any{"type": "exit", "code": s.ExitCode()})
					return nil
				}
			}
		case raw, ok := <-in:
			if !ok {
				return nil
			}
			var f inputFrame
			if json.Unmarshal(raw, &f) != nil {
				continue
			}
			switch f.Type {
			case "input":
				data, err := base64.StdEncoding.DecodeString(f.Data)
				if err != nil || len(data) == 0 {
					continue
				}
				_ = s.Write(data)
			case "resize":
				if cols, rows, err := checkSize(f.Cols, f.Rows); err == nil {
					_ = s.Resize(cols, rows)
				}
			}
		}
	}
}

func checkSize(cols, rows int) (uint16, uint16, error) {
	if cols < 1 || cols > 1000 || rows < 1 || rows > 1000 {
		return 0, 0, rpc.Errorf(rpc.Invalid, "The terminal size must be between 1 and 1000 columns and rows.")
	}
	return uint16(cols), uint16(rows), nil
}

var (
	hostRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.:_-]{0,251}[A-Za-z0-9])?$`)
	userRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}\$?$`)
)

func cleanName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if len(n) > 64 {
		return "", rpc.Errorf(rpc.Invalid, "The session name can have at most 64 characters.")
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f {
			return "", rpc.Errorf(rpc.Invalid, "The session name cannot contain control characters.")
		}
	}
	return n, nil
}

// buildSpec validates the parameters and builds the process to run.
func buildSpec(p createParams, admin bool) (spec, error) {
	cols, rows := 80, 24
	if p.Cols != 0 || p.Rows != 0 {
		cols, rows = p.Cols, p.Rows
	}
	c16, r16, err := checkSize(cols, rows)
	if err != nil {
		return spec{}, err
	}
	name, err := cleanName(p.Name)
	if err != nil {
		return spec{}, err
	}
	u, err := user.Current()
	if err != nil {
		return spec{}, rpc.Errorf(rpc.Internal, "Could not look up the current user: %v", err)
	}
	home := u.HomeDir
	if home == "" {
		home = "/"
	}
	host, _ := os.Hostname()
	kind := KindLocal
	if admin {
		kind = KindRoot
	}
	sp := spec{Cols: c16, Rows: r16}
	switch p.Kind {
	case "", "local", "root":
		if p.Kind == "root" && !admin {
			return spec{}, rpc.Errorf(rpc.NeedsAdmin, "A root shell needs administrator rights.")
		}
		shell, err := resolveShell(p.Shell, u.Username)
		if err != nil {
			return spec{}, err
		}
		dir := home
		if p.Cwd != "" {
			if !filepath.IsAbs(p.Cwd) {
				return spec{}, rpc.Errorf(rpc.Invalid, "The start folder must be an absolute path.")
			}
			fi, err := os.Stat(p.Cwd)
			if err != nil || !fi.IsDir() {
				return spec{}, rpc.Errorf(rpc.NotFound, "The folder %s does not exist.", p.Cwd)
			}
			dir = p.Cwd
		}
		base := filepath.Base(shell)
		sp.Kind, sp.Path, sp.Dir, sp.Shell = kind, shell, dir, base
		sp.Argv = []string{"-" + base} // leading dash: login shell
		if name == "" {
			name = u.Username + "@" + host
		}
		sp.Env = termEnv(u, home, shell)
	case "ssh":
		if !hostRe.MatchString(p.Host) {
			return spec{}, rpc.Errorf(rpc.Invalid, "Enter a valid host name or IP address.")
		}
		if p.User != "" && !userRe.MatchString(p.User) {
			return spec{}, rpc.Errorf(rpc.Invalid, "Enter a valid SSH user name.")
		}
		if p.Port != 0 && (p.Port < 1 || p.Port > 65535) {
			return spec{}, rpc.Errorf(rpc.Invalid, "The SSH port must be between 1 and 65535.")
		}
		ssh, err := sys.LookPath("ssh")
		if err != nil {
			return spec{}, rpc.Errorf(rpc.Unavailable, "The ssh client is not installed. Install openssh and try again.")
		}
		argv := []string{"ssh", "-o", "ServerAliveInterval=30"}
		if p.Port != 0 {
			argv = append(argv, "-p", strconv.Itoa(p.Port))
		}
		if p.User != "" {
			argv = append(argv, "-l", p.User)
		}
		argv = append(argv, "--", p.Host)
		sp.Kind, sp.Path, sp.Argv, sp.Dir, sp.Shell = KindSSH, ssh, argv, home, "ssh"
		if name == "" {
			name = p.Host
		}
		sp.Env = termEnv(u, home, "")
	default:
		return spec{}, rpc.Errorf(rpc.Invalid, "Unknown session kind %q. Use local, root or ssh.", p.Kind)
	}
	sp.Name = name
	return sp, nil
}

// resolveShell picks the requested shell (must be listed in /etc/shells) or
// the login shell from /etc/passwd, falling back to /bin/sh.
func resolveShell(want, username string) (string, error) {
	if want != "" {
		if !filepath.IsAbs(want) || !shellListed(want) {
			return "", rpc.Errorf(rpc.Invalid, "%s is not an allowed shell. Pick one listed in /etc/shells.", want)
		}
		return want, nil
	}
	if sh := passwdShell(username); sh != "" {
		return sh, nil
	}
	return "/bin/sh", nil
}

func shellListed(path string) bool {
	f, err := os.Open("/etc/shells")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == path {
			fi, err := os.Stat(path)
			return err == nil && fi.Mode()&0o111 != 0
		}
	}
	return false
}

func passwdShell(username string) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	return parsePasswdShell(f, username)
}

func parsePasswdShell(r io.Reader, username string) string {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), ":")
		if len(parts) >= 7 && parts[0] == username {
			sh := strings.TrimSpace(parts[6])
			if sh == "" || strings.HasSuffix(sh, "/nologin") || strings.HasSuffix(sh, "/false") || !filepath.IsAbs(sh) {
				return ""
			}
			if fi, err := os.Stat(sh); err != nil || fi.Mode()&0o111 == 0 {
				return ""
			}
			return sh
		}
	}
	return ""
}

// systemLang reads LANG from the environment or /etc/locale.conf.
func systemLang() string {
	if v := os.Getenv("LANG"); v != "" && v != "C" {
		return v
	}
	if f, err := os.Open("/etc/locale.conf"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "LANG="); ok {
				v = strings.Trim(v, `"'`)
				if v != "" {
					return v
				}
			}
		}
	}
	return "C.UTF-8"
}

func termEnv(u *user.User, home, shell string) []string {
	env := []string{
		"PATH=" + sys.SafePath,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=" + systemLang(),
		"HOME=" + home,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
	}
	if shell != "" {
		env = append(env, "SHELL="+shell)
	}
	if v := os.Getenv("TZ"); v != "" {
		env = append(env, "TZ="+v)
	}
	rt := fmt.Sprintf("/run/user/%s", u.Uid)
	if fi, err := os.Stat(rt); err == nil && fi.IsDir() {
		env = append(env, "XDG_RUNTIME_DIR="+rt)
	}
	return env
}
