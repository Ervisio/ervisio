// Package logs implements the bridge methods of the Logs section (logs.*):
// the list of sources, searching the systemd journal and log files, a level
// histogram, live following and the lines around an entry. See
// docs/api/logs.md.
package logs

import (
	"context"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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
	r.Handle("logs.query", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p queryParams
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return p.run(ctx, c.Admin)
	})
	r.Handle("logs.histogram", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p histParams
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return p.run(ctx, c.Admin)
	})
	r.Handle("logs.context", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p contextParams
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return p.run(ctx, c.Admin)
	})
	r.Stream("logs.follow", rpc.User, follow)
}
