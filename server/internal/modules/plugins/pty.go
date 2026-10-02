package plugins

import (
	"context"
	"os/exec"

	"github.com/ervisio/ervisio/server/internal/modules/terminal"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// PTYParams are the params of plugins.pty.
type PTYParams struct {
	ExecParams
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// runPTY runs a command declared "pty": true in a pseudo-terminal for the
// lifetime of the stream (terminal.RunPTY): binary output events, input
// frames {"type":"input","data":<base64>} and {"type":"resize","cols","rows"},
// then {"type":"exit","code"}. Same checks as plugins.exec.
func runPTY(ctx context.Context, c *rpc.Call, s rpc.Stream, p PTYParams) error {
	r, err := resolve(c, p.ExecParams, true)
	if err != nil {
		return err
	}
	path, err := sys.LookPath(r.argv[0])
	if err != nil {
		return err
	}
	cols, rows := p.Cols, p.Rows
	if cols == 0 && rows == 0 {
		cols, rows = 80, 24
	}
	// No sys.Cmd: its process group setting conflicts with the pty's new
	// session. RunPTY kills the session's process group instead.
	cmd := &exec.Cmd{Path: path, Args: r.argv, Env: sys.Env("TERM=xterm-256color", "COLORTERM=truecolor")}
	return terminal.RunPTY(ctx, cmd, cols, rows, s)
}
