package plugins

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

const (
	maxExecOutput  = 4 << 20
	defaultTimeout = 30 * time.Second
	maxArgLen      = 4096
)

// ExecParams are the params of plugins.exec and plugins.execStream.
type ExecParams struct {
	Plugin  string   `json:"plugin"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	// Env and EnvSocket: see HTTPParams. A remote command (manifest
	// "remote") gets the tunnel in its {env} argv item.
	Env       string `json:"env,omitempty"`
	EnvSocket string `json:"envSocket,omitempty"`
	// TimeoutSec is set by the job runner for a step that declares
	// timeoutSec. It is honoured only when some job of the plugin's manifest
	// has a step running this command with a timeoutSec of at least this
	// value (so a caller cannot lengthen a command beyond what the manifest
	// declared).
	TimeoutSec int `json:"timeoutSec,omitempty"`
}

// ExecResult is the result of plugins.exec.
type ExecResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exitCode"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Substitute builds the final argv of a command: every {N} slot is replaced by
// the Nth argument after checking it against the declared pattern. The number
// of arguments must equal the number of declared slots.
func Substitute(c *Command, args []string) ([]string, error) {
	if len(args) != len(c.Args) {
		return nil, fmt.Errorf("%q takes %d argument(s), got %d", c.Name, len(c.Args), len(args))
	}
	for i, spec := range c.Args {
		if err := checkArg(c, spec, i, args[i]); err != nil {
			return nil, err
		}
	}
	out := make([]string, len(c.Argv))
	out[0] = c.Argv[0]
	for i := 1; i < len(c.Argv); i++ {
		out[i] = slotRe.ReplaceAllStringFunc(c.Argv[i], func(s string) string {
			n, _ := strconv.Atoi(s[1 : len(s)-1])
			return args[n]
		})
	}
	return out, nil
}

// checkArg validates the i-th argument against its declared slot.
func checkArg(c *Command, spec ArgSpec, i int, a string) error {
	max := spec.MaxLen
	if max == 0 {
		max = 256
	}
	if len(a) > max || len(a) > maxArgLen || strings.ContainsRune(a, 0) {
		return fmt.Errorf("argument %d is too long or contains a NUL byte", i+1)
	}
	if strings.HasPrefix(a, "-") && !spec.AllowDash {
		return fmt.Errorf("argument %d (%q) may not start with a dash", i+1, a)
	}
	re := spec.re
	if re == nil { // manifest built without ParseManifest (tests)
		var err error
		if re, err = compileSpec(spec.Pattern); err != nil {
			return err
		}
	}
	if !re.MatchString(a) {
		return fmt.Errorf("argument %d (%q) is not allowed for %q", i+1, a, c.Name)
	}
	return nil
}

// resolved is an authorised, ready-to-run command.
type resolved struct {
	argv    []string
	timeout time.Duration
}

// resolve finds the plugin and command, checks enabled/visible/level and substitutes the arguments.
// pty commands run only through plugins.pty (wantPTY), and plugins.pty runs only pty commands.
func resolve(c *rpc.Call, p ExecParams, wantPTY bool) (*resolved, error) {
	if !idRe.MatchString(p.Plugin) || !cmdNameRe.MatchString(p.Command) {
		return nil, rpc.Errorf(rpc.Invalid, "Give a plugin id and the name of one of its commands.")
	}
	f, who, err := authorize(c, p.Plugin)
	if err != nil {
		return nil, err
	}
	m := f.M
	cmd, err := findCommand(m, p.Command, wantPTY)
	if err != nil {
		return nil, err
	}
	if err := checkRights(c, p, m, cmd, who); err != nil {
		return nil, err
	}
	argv, err := Substitute(cmd, p.Args)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", err)
	}
	argv = remoteArgv(cmd, argv, p.EnvSocket)
	to, err := commandTimeout(m, cmd, p)
	if err != nil {
		return nil, err
	}
	return &resolved{argv: argv, timeout: to}, nil
}

// findCommand returns the manifest command called name, which must be a pty
// command exactly when wantPTY.
func findCommand(m *Manifest, name string, wantPTY bool) (*Command, error) {
	var cmd *Command
	for i := range m.Capabilities.Commands {
		if m.Capabilities.Commands[i].Name == name {
			cmd = &m.Capabilities.Commands[i]
		}
	}
	if cmd == nil {
		return nil, rpc.Errorf(rpc.NotFound, "%s does not declare a command %q.", m.Name, name)
	}
	if cmd.PTY != wantPTY {
		if cmd.PTY {
			return nil, rpc.Errorf(rpc.Invalid, "%s declares %q as a terminal command; open it with sdk.api.pty.", m.Name, name)
		}
		return nil, rpc.Errorf(rpc.Invalid, "%s does not declare %q as a terminal (pty) command.", m.Name, name)
	}
	return cmd, nil
}

// checkRights checks that the caller may run cmd: on an environment, or with
// the rights the command declares.
func checkRights(c *rpc.Call, p ExecParams, m *Manifest, cmd *Command, who caller) error {
	if p.EnvSocket != "" {
		// Against an environment the docker program only talks to the user's
		// own tunnel: no administrator rights are needed or used.
		if cmd.Remote == "" || c.Admin {
			return rpc.Errorf(rpc.Forbidden, "%s does not allow %q to run on an environment.", m.Name, p.Command)
		}
		return envSocketOK(p.EnvSocket)
	}
	// A command declared as user-level never runs on the root bridge: the
	// plugin cannot raise its own rights by asking for admin.
	if c.Admin && !cmd.Admin {
		return rpc.Errorf(rpc.Forbidden, "%s declares %q as a user command; it does not run with administrator rights.", m.Name, p.Command)
	}
	root := c.Admin || os.Geteuid() == 0
	if cmd.Admin && !root && !(cmd.AdminUnlessGroup != "" && who.Groups[cmd.AdminUnlessGroup]) {
		return rpc.Errorf(rpc.NeedsAdmin, "%s needs administrator rights to run %q.", m.Name, p.Command)
	}
	return nil
}

// commandTimeout is the command's own timeout, or the one a job step asks
// for when the manifest allows it.
func commandTimeout(m *Manifest, cmd *Command, p ExecParams) (time.Duration, error) {
	to := defaultTimeout
	if cmd.TimeoutSec > 0 {
		to = time.Duration(cmd.TimeoutSec) * time.Second
	}
	if p.TimeoutSec != 0 {
		if !m.jobStepAllows(cmd.Name, p.TimeoutSec) {
			return 0, rpc.Errorf(rpc.Forbidden, "%s declares no job step that lets %q run for %d seconds.", m.Name, p.Command, p.TimeoutSec)
		}
		to = time.Duration(p.TimeoutSec) * time.Second
	}
	return to, nil
}

// resolveCmd resolves the command and builds its exec.Cmd, without timeout.
func resolveCmd(ctx context.Context, c *rpc.Call, p ExecParams) (*resolved, *exec.Cmd, error) {
	r, err := resolve(c, p, false)
	if err != nil {
		return nil, nil, err
	}
	cmd, err := sys.Cmd{Name: r.argv[0], Args: r.argv[1:], Timeout: -1}.Command(ctx)
	if err != nil {
		return nil, nil, err
	}
	return r, cmd, nil
}

func runExec(ctx context.Context, c *rpc.Call, p ExecParams) (*ExecResult, error) {
	r, err := resolve(c, p, false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	cmd, err := sys.Cmd{Name: r.argv[0], Args: r.argv[1:], Timeout: -1}.Command(ctx)
	if err != nil {
		return nil, err
	}
	var out, errb limitBuf
	out.max, errb.max = maxExecOutput, maxExecOutput
	cmd.Stdout, cmd.Stderr = &out, &errb
	res := &ExecResult{}
	err = cmd.Run()
	res.Stdout, res.Stderr, res.Truncated = out.buf.String(), errb.buf.String(), out.over || errb.over
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return nil, rpc.Errorf(rpc.Unavailable, "%s timed out after %s.", p.Command, r.timeout)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.As(err, &ee):
			res.ExitCode = ee.ExitCode()
		default:
			return nil, rpc.Errorf(rpc.Unavailable, "Could not run %s: %v", r.argv[0], err)
		}
	}
	return res, nil
}

type limitBuf struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *limitBuf) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); len(p) > room {
		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.over = true
	} else {
		b.buf.Write(p)
	}
	return len(p), nil
}

// runStream runs the command and sends {"stream":"stdout"|"stderr","line":…} per
// line, then {"exit":code}. There is no timeout; the client closes the stream.
func runStream(ctx context.Context, c *rpc.Call, s rpc.Stream, p ExecParams) error {
	r, cmd, err := resolveCmd(ctx, c, p)
	if err != nil {
		return err
	}
	so, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	se, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return rpc.Errorf(rpc.Unavailable, "Could not run %s: %v", r.argv[0], err)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)
	go pumpLines(cmd, s, &mu, &wg, "stdout", so)
	go pumpLines(cmd, s, &mu, &wg, "stderr", se)
	wg.Wait()
	code := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if ctx.Err() == nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return s.Send(map[string]int{"exit": code})
}

// pumpLines sends each line of rd as a stream event; a failed send kills the
// command. mu serialises the sends of both pumps.
func pumpLines(cmd *exec.Cmd, s rpc.Stream, mu *sync.Mutex, wg *sync.WaitGroup, name string, rd io.Reader) {
	defer wg.Done()
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		mu.Lock()
		err := s.Send(map[string]string{"stream": name, "line": sc.Text()})
		mu.Unlock()
		if err != nil {
			_ = cmd.Process.Kill()
			return
		}
	}
}
