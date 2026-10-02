// Package overview implements the Overview dashboard's own bridge methods:
// things that need attention (alerts), user-defined actions and the top
// processes. Host information and live metrics live in the system module.
package overview

import (
	"context"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Register adds the overview.* methods.
func Register(r *rpc.Registry) {
	al := newAlerter()
	r.Handle("overview.alerts", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return al.collect(ctx), nil
	})
	// actionRun is a User method on purpose: the client passes admin:true to
	// run it in the root bridge for actions flagged "run as administrator".
	r.Handle("overview.actionRun", rpc.User, actionRun)
	r.Handle("overview.processes", rpc.User, processes)
	r.Handle("overview.unitStatus", rpc.User, unitStatus)
	r.Handle("overview.logTail", rpc.User, logTail)
}
