package terminal

import (
	"context"
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
	defer func() { _ = ptmx.Close() }()
	pid := cmd.Process.Pid

	stop := make(chan struct{})
	defer close(stop) // runs before ptmx.Close: the reader never blocks on out
	out := readPTY(ptmx, stop)
	exit := waitExit(cmd)
	kill := func() { stopRun(pid, exit.done) }
	write := func(data []byte) { _, _ = ptmx.Write(data) }
	resize := func(cols, rows uint16) { _ = resizePTY(ptmx, cols, rows) }

	in := st.Input()
	exited := exit.done
	var drain <-chan time.Time // set once the process ended: output still buffered gets a moment
	for {
		select {
		case <-ctx.Done():
			kill()
			return nil
		case b, ok := <-out:
			if !ok {
				<-exit.done
				return st.Send(map[string]any{"type": "exit", "code": exit.code})
			}
			if err := st.SendBytes(b); err != nil {
				kill()
				return nil
			}
		case <-exited:
			// A child that keeps the pty open must not hold the stream.
			exited = nil
			drain = time.After(runExitDrain)
		case <-drain:
			hangupProc(pid, false)
			return st.Send(map[string]any{"type": "exit", "code": exit.code})
		case raw, ok := <-in:
			if !ok {
				kill()
				return nil
			}
			applyInputFrame(raw, write, resize)
		}
	}
}

const (
	ptyReadSize  = 32 << 10
	runKillGrace = time.Second
	runExitDrain = 500 * time.Millisecond
)

// readPTY copies the pty output to the returned channel, closed on EOF or
// read error. It gives up a pending send once stop is closed.
func readPTY(ptmx ptyConn, stop <-chan struct{}) <-chan []byte {
	out := make(chan []byte, 64)
	go func() {
		defer close(out)
		buf := make([]byte, ptyReadSize)
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
	return out
}

// exitStatus is the outcome of a process; code is valid once done is closed.
type exitStatus struct {
	done chan struct{}
	code int
}

func waitExit(cmd *exec.Cmd) *exitStatus {
	exit := &exitStatus{done: make(chan struct{})}
	go func() {
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				exit.code = ee.ExitCode()
			} else {
				exit.code = -1
			}
		}
		close(exit.done)
	}()
	return exit
}

// stopRun hangs up the process group and kills it if it outlives the grace
// period, then waits for the process to be reaped.
func stopRun(pid int, waited <-chan struct{}) {
	hangupProc(pid, false)
	select {
	case <-waited:
		return
	case <-time.After(runKillGrace):
	}
	killProc(pid)
	<-waited
}
