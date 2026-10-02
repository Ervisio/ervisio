package bridge

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// NewRemote wraps the connection to a bridge that runs on another Ervisio
// server (a paired environment): the bridge protocol flows over conn, whose
// first line is the other bridge's hello. Closing the Proc closes conn.
func NewRemote(ctx context.Context, conn io.ReadWriteCloser, tag string) (*Proc, error) {
	p := &Proc{stdin: conn, waited: make(chan struct{}), tag: tag}
	p.Client = rpc.NewClient(conn, conn, nil)
	ctx, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	h, err := p.Client.Hello(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("the other server's bridge did not start: %w", err)
	}
	p.Hello = h
	go func() {
		<-p.Client.Done()
		close(p.waited)
	}()
	return p, nil
}

// Raw is a started user bridge whose protocol is relayed as is (the
// pairing endpoint hands it to another server, which filters what it sends).
type Raw struct {
	Stdin  io.WriteCloser
	Stdout io.Reader
	cmd    *exec.Cmd
	done   chan struct{}
	once   sync.Once
}

// StartRaw starts the user bridge like StartUser but returns its pipes
// without reading the hello line.
func StartRaw(s *Spec) (*Raw, error) {
	var cmd *exec.Cmd
	if s.SessionHelper != "" && s.SwitchUser {
		cmd = s.helperCommand()
	} else {
		cmd = s.command(s.Bridge, s.bridgeArgs(false)...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start bridge: %w", err)
	}
	lg := s.logger()
	tag := "paired bridge " + s.Account.Name
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 4096), 64<<10)
		for sc.Scan() {
			lg.Printf("[%s] %s", tag, sc.Text())
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()
	r := &Raw{Stdin: stdin, Stdout: stdout, cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(r.done)
	}()
	return r, nil
}

// Stop closes the bridge's stdin and kills it if it does not exit.
func (r *Raw) Stop() {
	r.once.Do(func() {
		_ = r.Stdin.Close()
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			if r.cmd.Process != nil {
				_ = r.cmd.Process.Kill()
			}
			<-r.done
		}
	})
}

// Done is closed when the bridge exited.
func (r *Raw) Done() <-chan struct{} { return r.done }
