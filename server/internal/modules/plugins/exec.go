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

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
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
		a := args[i]
		max := spec.MaxLen
		if max == 0 {
			max = 256
		}
		if len(a) > max || len(a) > maxArgLen || strings.ContainsRune(a, 0) {
			return nil, fmt.Errorf("argument %d is too long or contains a NUL byte", i+1)
		}
		if strings.HasPrefix(a, "-") && !spec.AllowDash {
			return nil, fmt.Errorf("argument %d (%q) may not start with a dash", i+1, a)
		}
		re := spec.re
		if re == nil { // manifest built without ParseManifest (tests)
			var err error
			if re, err = compileSpec(spec.Pattern); err != nil {
				return nil, err
			}
		}
		if !re.MatchString(a) {
			return nil, fmt.Errorf("argument %d (%q) is not allowed for %q", i+1, a, c.Name)
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

// resolved is an authorised, ready-to-run command.
type resolved struct {
	argv    []string
	timeout time.Duration
}

// resolve finds the plugin and command, checks enabled/visible/level and substitutes the arguments.
func resolve(c *rpc.Call, p ExecParams) (*resolved, error) {
	if !idRe.MatchString(p.Plugin) || !cmdNameRe.MatchString(p.Command) {
		return nil, rpc.Errorf(rpc.Invalid, "Give a plugin id and the name of one of its commands.")
	}
	pol := readPolicy()
	f := find(pol, p.Plugin)
	if f == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", p.Plugin)
	}
	m := f.M
	if !readState().isEnabled(m.ID) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is turned off. Enable it in Plugins first.", m.Name)
	}
	if !pol.AllowUnsigned && !f.Sig.Verified {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is not signed and this server only runs signed plugins.", m.Name)
	}
	who := currentCaller(c.Admin)
	if !who.canSee(m) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is not available to your account.", m.Name)
	}
	var cmd *Command
	for i := range m.Capabilities.Commands {
		if m.Capabilities.Commands[i].Name == p.Command {
			cmd = &m.Capabilities.Commands[i]
		}
	}
	if cmd == nil {
		return nil, rpc.Errorf(rpc.NotFound, "%s does not declare a command %q.", m.Name, p.Command)
	}
	root := c.Admin || os.Geteuid() == 0
	if cmd.Admin && !root && !(cmd.AdminUnlessGroup != "" && who.Groups[cmd.AdminUnlessGroup]) {
		return nil, rpc.Errorf(rpc.NeedsAdmin, "%s needs administrator rights to run %q.", m.Name, p.Command)
	}
	argv, err := Substitute(cmd, p.Args)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", err)
	}
	to := defaultTimeout
	if cmd.TimeoutSec > 0 {
		to = time.Duration(cmd.TimeoutSec) * time.Second
	}
	return &resolved{argv: argv, timeout: to}, nil
}

func runExec(ctx context.Context, c *rpc.Call, p ExecParams) (*ExecResult, error) {
	r, err := resolve(c, p)
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
	r, err := resolve(c, p)
	if err != nil {
		return err
	}
	cmd, err := sys.Cmd{Name: r.argv[0], Args: r.argv[1:], Timeout: -1}.Command(ctx)
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
	pump := func(name string, rd io.Reader) {
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
	wg.Add(2)
	go pump("stdout", so)
	go pump("stderr", se)
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
