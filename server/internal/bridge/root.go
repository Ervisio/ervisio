package bridge

import (
	"context"
	"fmt"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// StartRoot starts a root bridge directly, without sudo and without a
// password: the daemon is root already. It exists for plugin jobs that an
// administrator approved (internal/jobs); a user's interactive admin
// rights still go through StartAdmin. spec.Account is root and
// spec.SwitchUser is false.
func StartRoot(ctx context.Context, s *Spec) (*Proc, error) {
	if !Privileged() {
		return nil, rpc.Errorf(rpc.Unavailable, "Steps that need administrator rights run only when the daemon runs as root (Windows: as SYSTEM or elevated).")
	}
	cmd := s.command(s.Bridge, s.bridgeArgs(true)...)
	// The root bridge never starts in a user's home.
	cmd.Dir = rootDir()
	p, _, err := s.start(cmd, "root bridge (job)", nil)
	if err != nil {
		return nil, err
	}
	h, err := awaitHello(ctx, p, "root bridge")
	if err != nil {
		return nil, err
	}
	if !h.Admin || !rootHello(h) {
		p.Stop()
		return nil, fmt.Errorf("the job's bridge is not running as root")
	}
	p.Hello = h
	return p, nil
}
