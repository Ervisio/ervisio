package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/ervisio/ervisio/server/internal/envs"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Environments, bridge side. The daemon owns the connections: for a call
// that targets an environment it checks the access list, then calls
// plugins.envCheck here (the plugin must be usable by this user and the
// capability or command must opt in with "remote"), and finally hands the
// bridge the path of a tunnel socket in the "envSocket" param. The bridge
// then talks to that socket instead of the capability's own, or puts it in
// the command's {env} item. For another Ervisio server the call goes to the
// bridge running there and none of this is used.

// envSocketOK accepts a tunnel socket path: absolute and clean, an actual
// socket, owned by the account this bridge runs as.
func envSocketOK(p string) error {
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 107 {
		return rpc.Errorf(rpc.Invalid, "The environment socket is not valid.")
	}
	fi, err := os.Lstat(p)
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return rpc.Errorf(rpc.Unavailable, "The environment's tunnel is not available. Try again.")
	}
	if !socketOwnedByUs(p, fi) {
		return rpc.Errorf(rpc.Forbidden, "The environment socket is not yours.")
	}
	return nil
}

// fileMethods are the plugin file methods that may take an environment.
var fileMethods = map[string]bool{
	"plugins.readFile": true, "plugins.writeFile": true, "plugins.listDir": true,
	"plugins.mkdir": true, "plugins.remove": true,
}

// envCheckParams are the params of plugins.envCheck.
type envCheckParams struct {
	Method string `json:"method"`
	Kind   string `json:"kind"`
	// Params are the original params of the call (plugin, name or command).
	Params json.RawMessage `json:"params"`
}

// envCheck implements plugins.envCheck: may this user, for this plugin and
// capability or command, use an environment of this kind?
func envCheck(_ context.Context, c *rpc.Call) (any, error) {
	var p envCheckParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	var q struct {
		Plugin  string `json:"plugin"`
		Name    string `json:"name"`
		Command string `json:"command"`
		// download/upload nest the HTTP request.
		Req *struct {
			Name string `json:"name"`
		} `json:"req"`
		Request *struct {
			Name string `json:"name"`
		} `json:"request"`
	}
	if len(p.Params) > 0 {
		_ = json.Unmarshal(p.Params, &q)
	}
	if q.Name == "" && q.Req != nil {
		q.Name = q.Req.Name
	}
	if q.Name == "" && q.Request != nil {
		q.Name = q.Request.Name
	}
	f, _, err := authorize(c, q.Plugin)
	if err != nil {
		return nil, err
	}
	m := f.M
	family := ""
	switch {
	case fileMethods[p.Method]:
		// Files are not tunnelled: only another Ervisio server can run them
		// (its bridge applies its own manifest). The plugin must be one that
		// uses environments at all.
		if p.Kind != envs.KindErvisio {
			return nil, rpc.Errorf(rpc.Invalid, "Files are local for this kind of environment: leave out env for files calls (only an environment of kind Ervisio, a paired server, can read and write files).")
		}
		if len(m.Capabilities.Files.Read)+len(m.Capabilities.Files.Write) == 0 {
			return nil, rpc.Errorf(rpc.Forbidden, "%s declares no files.", m.Name)
		}
		for i := range m.Capabilities.HTTP {
			if m.Capabilities.HTTP[i].Remote != "" {
				family = m.Capabilities.HTTP[i].Remote
			}
		}
		for i := range m.Capabilities.Commands {
			if m.Capabilities.Commands[i].Remote != "" {
				family = m.Capabilities.Commands[i].Remote
			}
		}
		if family == "" {
			return nil, rpc.Errorf(rpc.Forbidden, "%s does not allow targeting an environment.", m.Name)
		}
	case q.Command != "":
		for i := range m.Capabilities.Commands {
			if m.Capabilities.Commands[i].Name == q.Command {
				family = m.Capabilities.Commands[i].Remote
			}
		}
		if family == "" {
			return nil, rpc.Errorf(rpc.Forbidden, "%s does not allow the command %q to run on an environment.", m.Name, q.Command)
		}
	case q.Name != "":
		for i := range m.Capabilities.HTTP {
			if m.Capabilities.HTTP[i].Name == q.Name {
				family = m.Capabilities.HTTP[i].Remote
			}
		}
		if family == "" {
			return nil, rpc.Errorf(rpc.Forbidden, "%s does not allow %q to target an environment.", m.Name, q.Name)
		}
	default:
		return nil, rpc.Errorf(rpc.Invalid, "Give an HTTP API name or a command.")
	}
	if !envs.ServesFamily(p.Kind, family) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s cannot use an environment of this kind for %q.", m.Name, family)
	}
	return map[string]string{"family": family}, nil
}

// remoteArgv replaces the {env} item of a remote command: with the tunnel
// of the environment, or the family's local address when there is none.
func remoteArgv(cmd *Command, argv []string, envSocket string) []string {
	if cmd.Remote == "" {
		return argv
	}
	val := envs.FamilyLocalHost[cmd.Remote]
	if envSocket != "" {
		val = "unix://" + envSocket
	}
	for i := 1; i < len(argv) && i < len(cmd.Argv); i++ {
		if cmd.Argv[i] == envPlaceholder { // the template's item, never a value an argument carried
			argv[i] = val
		}
	}
	return argv
}
