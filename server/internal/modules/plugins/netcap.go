package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/ervisio/ervisio/server/internal/envs"
)

// UnmarshalJSON reads capabilities with the usual strictness, and accepts
// capabilities.network as a list of hosts (SDK v2/v3) or as
// {"hosts": [...], "userHosts": true}: with userHosts the plugin may ask an
// administrator to approve more hosts (plugins.network.request).
func (c *Capabilities) UnmarshalJSON(b []byte) error {
	type plain Capabilities
	aux := struct {
		*plain
		Network json.RawMessage `json:"network"`
	}{plain: (*plain)(c)}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&aux); err != nil {
		return err
	}
	c.Network = nil
	raw := bytes.TrimSpace(aux.Network)
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
	case raw[0] == '[':
		if err := json.Unmarshal(raw, &c.Network); err != nil {
			return fmt.Errorf("network: %v", err)
		}
	case raw[0] == '{':
		var obj struct {
			Hosts     []string `json:"hosts"`
			UserHosts bool     `json:"userHosts"`
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&obj); err != nil {
			return fmt.Errorf("network is a list of hosts or {hosts, userHosts}: %v", err)
		}
		c.Network, c.UserHosts = obj.Hosts, obj.UserHosts
	default:
		return fmt.Errorf("network is a list of hosts or {hosts, userHosts}")
	}
	return nil
}

// envPlaceholder is the argv item of a remote command that stands for the
// Docker endpoint: the local socket without an environment, the
// environment's tunnel with one.
const envPlaceholder = "{env}"

func (h *HTTPAPI) validateRemote() error {
	if h.Remote == "" {
		return nil
	}
	if _, ok := envs.Families[h.Remote]; !ok {
		return fmt.Errorf(`remote %q is not a known family (use "docker")`, h.Remote)
	}
	return nil
}

func (c *Command) validateRemote() error {
	has := 0
	for i, a := range c.Argv {
		if a == envPlaceholder {
			if i == 0 {
				return fmt.Errorf("the program (argv[0]) cannot be {env}")
			}
			has++
		} else if strings.Contains(a, envPlaceholder) {
			return fmt.Errorf("{env} must be a whole argv item")
		}
	}
	if c.Remote == "" {
		if has > 0 {
			return fmt.Errorf(`argv uses {env} but the command does not declare "remote"`)
		}
		return nil
	}
	if _, ok := envs.Families[c.Remote]; !ok {
		return fmt.Errorf(`remote %q is not a known family (use "docker")`, c.Remote)
	}
	if has != 1 {
		return fmt.Errorf(`a "remote" command needs exactly one {env} item in argv (for example ["docker","-H","{env}",...]), found %d`, has)
	}
	if b := path.Base(c.Argv[0]); b != "docker" {
		return fmt.Errorf(`a "remote": "docker" command must run the docker program, not %q`, c.Argv[0])
	}
	return nil
}
