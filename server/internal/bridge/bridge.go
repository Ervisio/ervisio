// Package bridge starts linuxadmin-bridge processes for a user: the user
// bridge (with the user's uid/gid/groups) and the root bridge (through
// `sudo -S`, after the user typed their password).
package bridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

// HelloTimeout bounds how long we wait for a bridge to say hello.
const HelloTimeout = 10 * time.Second

// Spec describes how to start bridges for one account.
type Spec struct {
	// Bridge is the absolute path of the linuxadmin-bridge binary.
	Bridge string
	// Config is passed to the bridge with --config.
	Config string
	// Account is the signed-in user.
	Account *account.Account
	// SwitchUser runs the process with the account's credentials. It is
	// false in dev mode, where the daemon already runs as that user.
	SwitchUser bool
	// Sudo overrides the sudo binary (tests); "" = sudo from the safe PATH.
	Sudo   string
	Logger *log.Logger
}

// Proc is a running bridge.
type Proc struct {
	*rpc.Client
	Hello *rpc.Hello
	cmd   *exec.Cmd
	stdin io.Closer

	waitOnce sync.Once
	waited   chan struct{}
}

func (s *Spec) logger() *log.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return log.Default()
}

func (s *Spec) env() []string {
	a := s.Account
	env := []string{
		"PATH=" + sys.SafePath,
		"HOME=" + a.Home,
		"USER=" + a.Name,
		"LOGNAME=" + a.Name,
		"SHELL=" + a.Shell,
		"LANG=C.UTF-8",
	}
	rt := "/run/user/" + strconv.FormatUint(uint64(a.UID), 10)
	if fi, err := os.Stat(rt); err == nil && fi.IsDir() {
		env = append(env, "XDG_RUNTIME_DIR="+rt)
	}
	return env
}

func (s *Spec) command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = s.env()
	cmd.Dir = "/"
	if fi, err := os.Stat(s.Account.Home); err == nil && fi.IsDir() {
		cmd.Dir = s.Account.Home
	}
	attr := &syscall.SysProcAttr{
		// A new session: no controlling terminal (so sudo cannot prompt on
		// a tty) and no terminal signals from the daemon's console.
		Setsid:    true,
		Pdeathsig: syscall.SIGTERM,
	}
	if s.SwitchUser {
		attr.Credential = &syscall.Credential{
			Uid:    s.Account.UID,
			Gid:    s.Account.GID,
			Groups: s.Account.Groups,
		}
	}
	cmd.SysProcAttr = attr
	return cmd
}

func (s *Spec) bridgeArgs(admin bool) []string {
	args := []string{"--config", s.Config}
	if admin {
		args = append([]string{"--admin"}, args...)
	}
	return args
}

// start launches cmd and wires its pipes to an rpc.Client. stderr lines are
// passed to onStderr (which may be nil) and then logged.
func (s *Spec) start(cmd *exec.Cmd, tag string, onStderr func(string)) (*Proc, io.WriteCloser, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start %s: %w", filepath.Base(cmd.Path), err)
	}
	lg := s.logger()
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 4096), 64<<10)
		for sc.Scan() {
			line := sc.Text()
			if onStderr != nil {
				onStderr(line)
			}
			lg.Printf("[%s] %s", tag, line)
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()
	p := &Proc{cmd: cmd, stdin: stdin, waited: make(chan struct{})}
	p.Client = rpc.NewClient(stdout, stdin, lg)
	go func() {
		<-p.Client.Done()
		p.wait()
	}()
	return p, stdin, nil
}

func (p *Proc) wait() {
	p.waitOnce.Do(func() {
		_ = p.cmd.Wait()
		close(p.waited)
	})
}

// Stop closes the bridge's stdin, waits up to 3 s for it to exit and kills
// it otherwise.
func (p *Proc) Stop() {
	_ = p.stdin.Close()
	select {
	case <-p.waited:
		return
	case <-time.After(3 * time.Second):
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.waited:
	case <-time.After(2 * time.Second):
	}
}

// StartUser starts the user bridge and waits for its hello.
func StartUser(ctx context.Context, s *Spec) (*Proc, error) {
	cmd := s.command(s.Bridge, s.bridgeArgs(false)...)
	p, _, err := s.start(cmd, "bridge "+s.Account.Name, nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	h, err := p.Client.Hello(ctx)
	if err != nil {
		p.Stop()
		return nil, fmt.Errorf("bridge did not start: %w", err)
	}
	if uint32(h.UID) != s.Account.UID && s.SwitchUser {
		p.Stop()
		return nil, fmt.Errorf("bridge runs as uid %d, expected %d", h.UID, s.Account.UID)
	}
	p.Hello = h
	return p, nil
}

// sudo stderr messages (sudo runs with LC_ALL=C so they are in English).
var (
	sudoWrongPassword = []string{"Sorry, try again", "incorrect password", "Authentication failure", "a password is required"}
	sudoForbidden     = []string{"not in the sudoers file", "is not allowed to", "may not run sudo", "not allowed to execute"}
)

func classifySudo(line string) rpc.Code {
	for _, s := range sudoWrongPassword {
		if strings.Contains(line, s) {
			return rpc.Invalid
		}
	}
	for _, s := range sudoForbidden {
		if strings.Contains(line, s) {
			return rpc.Forbidden
		}
	}
	return ""
}

// StartAdmin runs `sudo -S -p ” -k -- bridge --admin` as the user, feeding
// the password on stdin, and waits for the root bridge's hello. Errors are
// *rpc.Error: Invalid for a wrong password, Forbidden when sudo refuses
// the user, Unavailable when sudo is missing or times out.
func StartAdmin(ctx context.Context, s *Spec, password string) (*Proc, error) {
	if password == "" || strings.ContainsAny(password, "\n\r\x00") || len(password) > 4096 {
		return nil, rpc.Errorf(rpc.Invalid, "invalid password")
	}
	sudo := s.Sudo
	if sudo == "" {
		var err error
		if sudo, err = sys.LookPath("sudo"); err != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "sudo is not installed")
		}
	}
	args := append([]string{"-S", "-p", "", "-k", "--", s.Bridge}, s.bridgeArgs(true)...)
	cmd := s.command(sudo, args...)
	// English messages so failures can be classified.
	cmd.Env = append(cmd.Env, "LC_ALL=C")

	verdict := make(chan rpc.Code, 1)
	var stderrMu sync.Mutex
	var stderrTail []string
	onStderr := func(line string) {
		stderrMu.Lock()
		if len(stderrTail) < 20 {
			stderrTail = append(stderrTail, line)
		}
		stderrMu.Unlock()
		if code := classifySudo(line); code != "" {
			select {
			case verdict <- code:
			default:
			}
		}
	}
	p, stdin, err := s.start(cmd, "root bridge "+s.Account.Name, onStderr)
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
	}
	if _, err := io.WriteString(stdin, password+"\n"); err != nil {
		p.Stop()
		return nil, rpc.Errorf(rpc.Unavailable, "sudo: %v", err)
	}

	ctx, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	helloCh := make(chan *rpc.Hello, 1)
	go func() {
		h, _ := p.Client.Hello(ctx)
		helloCh <- h
	}()
	fail := func(e *rpc.Error) (*Proc, error) {
		killGroup(p)
		p.Stop()
		return nil, e
	}
	select {
	case h := <-helloCh:
		if h == nil {
			// Exited (or timed out) before hello: let stderr settle, classify.
			select {
			case code := <-verdict:
				return fail(sudoError(code))
			case <-time.After(200 * time.Millisecond):
			}
			if ctx.Err() != nil {
				return fail(rpc.Errorf(rpc.Unavailable, "sudo did not answer in time"))
			}
			stderrMu.Lock()
			msg := strings.Join(stderrTail, "; ")
			stderrMu.Unlock()
			return fail(rpc.Errorf(rpc.Unavailable, "sudo failed: %s", msg))
		}
		if !h.Admin || h.UID != 0 {
			return fail(rpc.Errorf(rpc.Forbidden, "sudo did not grant root rights"))
		}
		p.Hello = h
		return p, nil
	case code := <-verdict:
		return fail(sudoError(code))
	}
}

func sudoError(code rpc.Code) *rpc.Error {
	if code == rpc.Forbidden {
		return rpc.Errorf(rpc.Forbidden, "this account is not allowed to use sudo")
	}
	return rpc.Errorf(rpc.Invalid, "wrong password")
}

// killGroup terminates sudo (it waits for a password retry on stdin).
func killGroup(p *Proc) {
	if p.cmd.Process == nil {
		return
	}
	err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ESRCH) {
		_ = p.cmd.Process.Kill()
	}
}
