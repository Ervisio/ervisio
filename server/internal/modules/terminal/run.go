package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// RunPTY runs cmd in a new pty of cols x rows for the lifetime of one
// stream, without a persistent session (used by plugins.pty). Output goes
// out as binary events; the stream's input frames are the ones of
// terminal.attach ({"type":"input","data":<base64>} and
// {"type":"resize","cols","rows"}). When the process ends the last event is
// {"type":"exit","code"}. Closing the stream hangs up and kills the
// process group. cmd must not set SysProcAttr: the pty makes the process a
// session leader.
func RunPTY(ctx context.Context, cmd *exec.Cmd, cols, rows int, st rpc.Stream) error {
	c16, r16, err := checkSize(cols, rows)
	if err != nil {
		return err
	}
	ptmx, err := startPTY(cmd, c16, r16)
	if err != nil {
		return rpc.Errorf(rpc.Unavailable, "Could not start %s: %v", cmd.Path, err)
	}
	defer ptmx.Close()
	pid := cmd.Process.Pid

	out := make(chan []byte, 64)
	stop := make(chan struct{})
	defer close(stop) // runs before ptmx.Close: the reader never blocks on out
	go func() {
		defer close(out)
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				select {
				case out <- append([]byte(nil), buf[:n]...):
				case <-stop:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	waited := make(chan struct{})
	code := 0
	go func() {
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		close(waited)
	}()

	kill := func() {
		hangupProc(pid, false)
		select {
		case <-waited:
			return
		case <-time.After(time.Second):
		}
		killProc(pid)
		<-waited
	}

	in := st.Input()
	exited := waited
	var drain <-chan time.Time // set once the process ended: output still buffered gets a moment
	for {
		select {
		case <-ctx.Done():
			kill()
			return nil
		case b, ok := <-out:
			if !ok {
				<-waited
				return st.Send(map[string]any{"type": "exit", "code": code})
			}
			if err := st.SendBytes(b); err != nil {
				kill()
				return nil
			}
		case <-exited:
			// A child that keeps the pty open must not hold the stream.
			exited = nil
			drain = time.After(500 * time.Millisecond)
		case <-drain:
			hangupProc(pid, false)
			return st.Send(map[string]any{"type": "exit", "code": code})
		case raw, ok := <-in:
			if !ok {
				kill()
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
				_, _ = ptmx.Write(data)
			case "resize":
				if cols, rows, err := checkSize(f.Cols, f.Rows); err == nil {
					_ = resizePTY(ptmx, cols, rows)
				}
			}
		}
	}
}
