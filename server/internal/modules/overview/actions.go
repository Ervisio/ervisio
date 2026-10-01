package overview

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

const (
	maxArgs          = 128
	maxArgLen        = 8192
	maxActionOutput  = 256 << 10
	defaultActionSec = 30
	maxActionSec     = 600
)

type actionParams struct {
	Argv []string `json:"argv"`
	// Timeout in seconds (default 30, at most 600).
	Timeout int `json:"timeout"`
}

// ActionResult is what overview.actionRun returns.
type ActionResult struct {
	OK         bool   `json:"ok"`
	ExitCode   int    `json:"exitCode"` // -1 when killed (timeout, signal)
	Output     string `json:"output"`   // stdout and stderr interleaved
	Truncated  bool   `json:"truncated,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

func validateArgv(argv []string) error {
	if len(argv) == 0 {
		return rpc.Errorf(rpc.Invalid, "The command is empty. Enter a command to run.")
	}
	if len(argv) > maxArgs {
		return rpc.Errorf(rpc.Invalid, "The command has too many arguments (at most %d).", maxArgs)
	}
	for i, a := range argv {
		if i == 0 && a == "" {
			return rpc.Errorf(rpc.Invalid, "The command is empty. Enter a command to run.")
		}
		if len(a) > maxArgLen || strings.ContainsRune(a, 0) {
			return rpc.Errorf(rpc.Invalid, "Argument %d is too long or contains a NUL character.", i+1)
		}
	}
	return nil
}

func actionRun(ctx context.Context, c *rpc.Call) (any, error) {
	var p actionParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := validateArgv(p.Argv); err != nil {
		return nil, err
	}
	sec := p.Timeout
	if sec <= 0 {
		sec = defaultActionSec
	}
	if sec > maxActionSec {
		sec = maxActionSec
	}
	dir := ""
	if h, err := os.UserHomeDir(); err == nil {
		if fi, err := os.Stat(h); err == nil && fi.IsDir() {
			dir = h
		}
	}
	return runArgv(ctx, p.Argv, time.Duration(sec)*time.Second, dir)
}

func runArgv(ctx context.Context, argv []string, timeout time.Duration, dir string) (*ActionResult, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := sys.Cmd{Name: argv[0], Args: argv[1:], Dir: dir, Timeout: -1}.Command(cctx)
	if err != nil {
		return nil, err
	}
	w := &capWriter{max: maxActionOutput}
	cmd.Stdout, cmd.Stderr = w, w // same writer: one goroutine copies both
	start := time.Now()
	runErr := cmd.Run()
	res := &ActionResult{Output: w.String(), Truncated: w.over, DurationMs: time.Since(start).Milliseconds()}
	switch {
	case runErr == nil:
		res.OK = true
	case errors.Is(cctx.Err(), context.DeadlineExceeded):
		res.ExitCode, res.TimedOut = -1, true
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			return nil, rpc.Errorf(rpc.Unavailable, "Could not start %s: %v", argv[0], runErr)
		}
	}
	return res, nil
}

type capWriter struct {
	mu   sync.Mutex
	b    []byte
	max  int
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := w.max - len(w.b); len(p) > room {
		if room > 0 {
			w.b = append(w.b, p[:room]...)
		}
		w.over = true
	} else {
		w.b = append(w.b, p...)
	}
	return len(p), nil
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.ToValidUTF8(string(w.b), "�")
}
