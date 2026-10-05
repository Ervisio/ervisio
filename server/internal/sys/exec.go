// Package sys holds helpers shared by bridge modules: running commands
// safely (argv only, sanitised environment, bounded output), reading
// /etc/os-release and detecting the primary IP address.
package sys

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// SafePath is the PATH used to resolve and run commands.
const SafePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// DefaultMaxOutput bounds captured stdout for Output (16 MiB).
const DefaultMaxOutput = 16 << 20

// DefaultTimeout bounds Run and Output when the context has no deadline.
const DefaultTimeout = 2 * time.Minute

// maxStderr is how much stderr is kept for error messages.
const maxStderr = 8 << 10

// keptEnv lists variables copied from the bridge environment.
var keptEnv = []string{"HOME", "USER", "LOGNAME", "SHELL", "TZ", "XDG_RUNTIME_DIR"}

// Env returns a sanitised environment: a fixed PATH, the C.UTF-8 locale for
// predictable output parsing, the identity variables of the current process,
// plus extra "KEY=value" entries (which override).
func Env(extra ...string) []string {
	env := []string{"PATH=" + SafePath, "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	for _, k := range keptEnv {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return mergeEnv(env, extra)
}

func mergeEnv(base, extra []string) []string {
	out := make([]string, 0, len(base)+len(extra))
	seen := map[string]bool{}
	for i := len(extra) - 1; i >= 0; i-- {
		k, _, ok := strings.Cut(extra[i], "=")
		if !ok || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, extra[i])
	}
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !seen[k] {
			out = append(out, kv)
		}
	}
	return out
}

// LookPath resolves name in SafePath. Absolute paths are used as given.
// A missing command yields an rpc.Unavailable error.
func LookPath(name string) (string, error) {
	if name == "" {
		return "", rpc.Errorf(rpc.Invalid, "empty command")
	}
	if strings.Contains(name, "/") {
		if !filepath.IsAbs(name) {
			return "", rpc.Errorf(rpc.Invalid, "command path must be absolute: %s", name)
		}
		if isExecutable(name) {
			return name, nil
		}
		return "", rpc.Errorf(rpc.Unavailable, "%s is not installed", name)
	}
	for _, dir := range filepath.SplitList(SafePath) {
		p := filepath.Join(dir, name)
		if isExecutable(p) {
			return p, nil
		}
	}
	return "", rpc.Errorf(rpc.Unavailable, "%s is not installed", name)
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// Cmd describes a command to run. Arguments are passed as argv, never
// through a shell.
type Cmd struct {
	Name string
	Args []string
	// Stdin is fed to the process (nil = /dev/null).
	Stdin io.Reader
	// Env holds extra "KEY=value" variables added to Env().
	Env []string
	// Dir is the working directory ("" = the bridge's).
	Dir string
	// MaxOutput bounds captured stdout (0 = DefaultMaxOutput).
	MaxOutput int
	// Timeout bounds Run/Output (0 = DefaultTimeout unless ctx has a deadline,
	// negative = none).
	Timeout time.Duration
}

// ExitError is returned when a command exits unsuccessfully.
type ExitError struct {
	Name   string
	Code   int // -1 when killed by a signal
	Stderr string
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("%s exited with status %d", e.Name, e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Name, msg)
}

// Command builds an *exec.Cmd with the resolved binary and sanitised env.
// The process gets its own process group and is killed with it when ctx ends.
func (c Cmd) Command(ctx context.Context) (*exec.Cmd, error) {
	path, err := LookPath(c.Name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, c.Args...)
	cmd.Env = Env(c.Env...)
	cmd.Dir = c.Dir
	cmd.Stdin = c.Stdin
	setProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

func (c Cmd) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.Timeout < 0 {
		return context.WithCancel(ctx)
	}
	if c.Timeout > 0 {
		return context.WithTimeout(ctx, c.Timeout)
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

// Output runs the command and returns its stdout (bounded by MaxOutput).
func (c Cmd) Output(ctx context.Context) ([]byte, error) {
	max := c.MaxOutput
	if max <= 0 {
		max = DefaultMaxOutput
	}
	stdout := &capBuffer{max: max}
	if err := c.run(ctx, stdout); err != nil {
		return stdout.buf.Bytes(), err
	}
	if stdout.over {
		return stdout.buf.Bytes(), rpc.Errorf(rpc.Internal, "%s: output larger than %d bytes", c.Name, max)
	}
	return stdout.buf.Bytes(), nil
}

// Run runs the command, discarding stdout.
func (c Cmd) Run(ctx context.Context) error { return c.run(ctx, nil) }

func (c Cmd) run(ctx context.Context, stdout io.Writer) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	cmd, err := c.Command(ctx)
	if err != nil {
		return err
	}
	stderr := &capBuffer{max: maxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return c.wrap(ctx, cmd.Run(), stderr.buf.String())
}

// Stream runs the command and calls onLine for each stdout line (without the
// trailing newline; lines longer than 1 MiB are split). Returning an error
// from onLine stops the command. There is no default timeout.
func (c Cmd) Stream(ctx context.Context, onLine func(line string) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, err := c.Command(ctx)
	if err != nil {
		return err
	}
	stderr := &capBuffer{max: maxStderr}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var cbErr error
	for sc.Scan() {
		if cbErr = onLine(sc.Text()); cbErr != nil {
			cancel()
			break
		}
	}
	if cbErr == nil && sc.Err() != nil && !errors.Is(sc.Err(), os.ErrClosed) {
		cbErr = sc.Err()
		cancel()
	}
	_, _ = io.Copy(io.Discard, out)
	werr := cmd.Wait()
	if cbErr != nil {
		return cbErr
	}
	return c.wrap(ctx, werr, stderr.buf.String())
}

func (c Cmd) wrap(ctx context.Context, err error, stderr string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return rpc.Errorf(rpc.Unavailable, "%s timed out", c.Name)
		}
		return ctx.Err()
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return &ExitError{Name: c.Name, Code: ee.ExitCode(), Stderr: stderr}
	}
	return err
}

// Run runs name with args (see Cmd.Run).
func Run(ctx context.Context, name string, args ...string) error {
	return Cmd{Name: name, Args: args}.Run(ctx)
}

// Output runs name with args and returns stdout (see Cmd.Output).
func Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return Cmd{Name: name, Args: args}.Output(ctx)
}

// Stream runs name with args and calls onLine per stdout line (see Cmd.Stream).
func Stream(ctx context.Context, name string, args []string, onLine func(line string) error) error {
	return Cmd{Name: name, Args: args}.Stream(ctx, onLine)
}

// capBuffer keeps at most max bytes and records overflow. It deliberately
// has no ReadFrom method so io.Copy goes through Write.
type capBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *capBuffer) Write(p []byte) (int, error) {
	room := b.max - b.buf.Len()
	if len(p) > room {
		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.over = true
		return len(p), nil
	}
	return b.buf.Write(p)
}
