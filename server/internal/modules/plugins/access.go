package plugins

import (
	"context"
	"regexp"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// trustLevel says whether a plugin's signature lets it run.
type trustLevel int

const (
	trustOK          trustLevel = iota // verified, or allow_unsigned is on
	trustDevUnsigned                   // unsigned dev folder, allowed because developer mode is on
	trustBlocked                       // unsigned or invalid while allow_unsigned is off
)

// trust applies plugins.allow_unsigned. Folders in the dev location (the
// repository's ./plugins under a --dev daemon, or folders loaded with
// plugins.loadDev) can only be scanned when developer mode is on; they may
// run unsigned so plugin authors can iterate, and the UI marks them.
func trust(p policy, f *Found) trustLevel {
	switch {
	case f.Sig.Verified || p.AllowUnsigned:
		return trustOK
	case f.Location == LocDev && devEnabled(p):
		return trustDevUnsigned
	}
	return trustBlocked
}

// authorize finds an enabled plugin the caller may use: it exists, is
// switched on, passes the signature policy and is visible to the caller.
// Every plugin-scoped method (exec, files, access) goes through it.
func authorize(c *rpc.Call, id string) (*Found, caller, error) {
	if !idRe.MatchString(id) {
		return nil, caller{}, rpc.Errorf(rpc.Invalid, "%q is not a plugin id.", id)
	}
	pol := readPolicy()
	f := find(pol, id)
	if f == nil {
		return nil, caller{}, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", id)
	}
	m := f.M
	if !readState().isEnabled(m.ID) {
		return nil, caller{}, rpc.Errorf(rpc.Forbidden, "%s is turned off. Enable it in Plugins first.", m.Name)
	}
	if trust(pol, f) == trustBlocked {
		return nil, caller{}, rpc.Errorf(rpc.Forbidden, "%s is not signed and this server only runs signed plugins.", m.Name)
	}
	who := currentCaller(c.Admin)
	if !who.canSee(m) {
		return nil, caller{}, rpc.Errorf(rpc.Forbidden, "%s is not available to your account.", m.Name)
	}
	return f, who, nil
}

// AccessInfo is the result of plugins.access.
type AccessInfo struct {
	ID string `json:"id"`
	// Network lists the hosts the plugin's sandboxed frame may connect to
	// (capabilities.network), already validated as CSP-safe host sources.
	Network []string `json:"network"`
}

// access implements plugins.access {id}: the daemon asks it before serving
// /plugins/<id>/… or the plugin frame, so a disabled, blocked or hidden
// plugin is never served (security review L2). Any refusal is not_found.
func access(_ context.Context, c *rpc.Call) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	f, _, err := authorize(c, p.ID)
	if err != nil {
		return nil, rpc.Errorf(rpc.NotFound, "not found")
	}
	net := []string{}
	for _, h := range f.M.Capabilities.Network {
		if NetworkHostRe.MatchString(h) {
			net = append(net, h)
		}
	}
	// Hosts an administrator approved for this plugin (userhosts.go); an
	// http approval is written with its scheme, the rest are https.
	if f.M.Capabilities.UserHosts {
		for _, a := range approvedFor(f.M.ID) {
			if a.Scheme == "http" {
				net = append(net, "http://"+a.Host)
			} else {
				net = append(net, a.Host)
			}
		}
	}
	return AccessInfo{ID: f.M.ID, Network: net}, nil
}

// NetworkHostRe is what capabilities.network accepts: a host name, an
// optional "*." wildcard prefix and an optional port. Entries are placed in
// the frame's Content-Security-Policy, so nothing else (no ';', quotes,
// schemes or paths) may get through.
var NetworkHostRe = regexp.MustCompile(`^(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*(:[0-9]{1,5})?$`)
