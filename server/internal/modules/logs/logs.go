// Package logs implements the bridge methods of the Logs section (logs.*):
// the list of sources, searching the systemd journal and log files, a level
// histogram, live following and the lines around an entry. See
// docs/api/logs.md.
package logs

import (
	"context"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Register adds the logs.* methods to the registry. Everything is user
// level: what the user's groups allow is shown, and calls with admin:true
// run in the root bridge and see everything.
func Register(r *rpc.Registry) {
	r.Handle("logs.sources", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Watchers []Watcher `json:"watchers"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return listSources(ctx, p.Watchers, c.Admin)
	})
	r.Handle("logs.query", rpc.User, bindAndRun(queryParams.run))
	r.Handle("logs.histogram", rpc.User, bindAndRun(histParams.run))
	r.Handle("logs.context", rpc.User, bindAndRun(contextParams.run))
	r.Stream("logs.follow", rpc.User, follow)
}

// bindAndRun turns a params type and its run method into a handler.
func bindAndRun[P any, R any](run func(P, context.Context, bool) (R, error)) rpc.Handler {
	return func(ctx context.Context, c *rpc.Call) (any, error) {
		var p P
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return run(p, ctx, c.Admin)
	}
}
