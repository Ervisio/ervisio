package plugins

import (
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// The daemon (internal/jobs, internal/server) runs plugin jobs and sends
// plugin notifications itself, outside any bridge. These helpers give it
// the same checks authorize applies in a bridge: the plugin exists, is
// switched on, passes the signature policy and is visible to the account.

// Resolve returns the manifest of an enabled plugin that may run.
func Resolve(id string) (*Manifest, error) {
	if !idRe.MatchString(id) {
		return nil, rpc.Errorf(rpc.Invalid, "%q is not a plugin id.", id)
	}
	pol := readPolicy()
	f := find(pol, id)
	if f == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", id)
	}
	if !readState().isEnabled(f.M.ID) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is turned off. Enable it in Plugins first.", f.M.Name)
	}
	if trust(pol, f) == trustBlocked {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is not signed and this server only runs signed plugins.", f.M.Name)
	}
	return f.M, nil
}

// CanBeUsedBy reports whether an account (its group names, and whether it
// can administer the machine) may use the plugin.
func (m *Manifest) CanBeUsedBy(groups map[string]bool, admin bool) bool {
	return caller{Groups: groups, Admin: admin}.canSee(m)
}
