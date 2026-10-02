package bridge

import (
	"context"
	"fmt"
	"os"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// StartRoot starts a root bridge directly, without sudo and without a
// password: the daemon is root already. It exists for plugin jobs that an
// administrator approved (internal/jobs); a user's interactive admin
// rights still go through StartAdmin. spec.Account is root and
// spec.SwitchUser is false.
func StartRoot(ctx context.Context, s *Spec) (*Proc, error) {
	if os.Geteuid() != 0 {
		return nil, rpc.Errorf(rpc.Unavailable, "Steps that need administrator rights run only when the daemon runs as root.")
	}
	cmd := s.command(s.Bridge, s.bridgeArgs(true)...)
	// The root bridge never starts in a user's home.
	cmd.Dir = "/"
	p, _, err := s.start(cmd, "root bridge (job)", nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	h, err := p.Client.Hello(ctx)
	if err != nil {
		p.Stop()
		return nil, fmt.Errorf("root bridge did not start: %w", err)
	}
	if !h.Admin || h.UID != 0 {
		p.Stop()
		return nil, fmt.Errorf("the job's bridge is not running as root")
	}
	p.Hello = h
	return p, nil
}
