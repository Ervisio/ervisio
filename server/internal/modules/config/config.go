// Package config exposes the daemon configuration file to the Settings
// section: config.get (any user, secrets omitted), config.set (admin, keeps a
// .bak backup) and config.rawDiff (preview of a change as a TOML diff).
package config

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
	cfg "github.com/Fonlogen/LinuxAdmin/server/internal/config"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// Path is the configuration file. The bridge sets it from --config before
// registering modules.
var Path = brand.ConfigPath

// State is the result of config.get and config.set.
type State struct {
	Path     string         `json:"path"`
	Exists   bool           `json:"exists"`
	Values   map[string]any `json:"values"`
	Defaults map[string]any `json:"defaults"`
	Keys     []cfg.Key      `json:"keys"`
	Warnings []string       `json:"warnings"`
}

// Register adds config.get, config.set and config.rawDiff.
func Register(r *rpc.Registry) {
	r.Handle("config.get", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return get()
	})
	r.Handle("config.set", rpc.Admin, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return set(p.Key, p.Value)
	})
	r.Handle("config.rawDiff", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Changes map[string]any `json:"changes"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return rawDiff(p.Changes)
	})
}

func load() (*cfg.Config, bool, []string, error) {
	c, exists, warn, err := cfg.Load(Path)
	if err != nil {
		if rpcErr := rpc.ToError(err, false); rpcErr.Code != rpc.Internal {
			return nil, false, nil, err
		}
		return nil, exists, nil, rpc.Errorf(rpc.Conflict, "the configuration file is invalid: %v", err)
	}
	return c, exists, warn, nil
}

func get() (*State, error) {
	c, exists, warn, err := load()
	if err != nil {
		return nil, err
	}
	var keys []cfg.Key
	for _, k := range cfg.Keys() {
		if !k.Secret {
			keys = append(keys, k)
		}
	}
	if warn == nil {
		warn = []string{}
	}
	return &State{Path: Path, Exists: exists, Values: c.Values(), Defaults: cfg.Default().Values(), Keys: keys, Warnings: warn}, nil
}

func set(key string, value any) (*State, error) {
	c, _, _, err := load()
	if err != nil {
		return nil, err
	}
	if err := c.Set(key, normalise(value)); err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", err)
	}
	if err := cfg.Save(Path, c); err != nil {
		return nil, err
	}
	return get()
}

// Diff is the result of config.rawDiff.
type Diff struct {
	Current  string `json:"current"`
	Proposed string `json:"proposed"`
	Diff     string `json:"diff"`
}

func rawDiff(changes map[string]any) (*Diff, error) {
	if len(changes) == 0 {
		return nil, rpc.Errorf(rpc.Invalid, "changes must be a non-empty object")
	}
	c, _, _, err := load()
	if err != nil {
		return nil, err
	}
	cur, err := cfg.Encode(c)
	if err != nil {
		return nil, err
	}
	next := c.Clone()
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := next.Set(k, normalise(changes[k])); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "%v", err)
		}
	}
	prop, err := cfg.Encode(next)
	if err != nil {
		return nil, err
	}
	return &Diff{Current: string(cur), Proposed: string(prop), Diff: cfg.Diff(string(cur), string(prop))}, nil
}

// normalise turns json.Number into float64 so config.Set sees JSON types.
func normalise(v any) any {
	if n, ok := v.(json.Number); ok {
		f, _ := n.Float64()
		return f
	}
	return v
}
