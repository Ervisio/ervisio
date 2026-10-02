package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// User-approved network hosts. A plugin with capabilities.network
// {"userHosts": true} may ask for a host that is not in its manifest
// (plugins.network.request); an administrator approves it once, for that
// exact host:port, over https unless approved as http. Approvals are listed
// and revoked in Settings > Plugins and become part of the plugin frame's
// Content-Security-Policy (the frame is reloaded after an approval).

// HostsPath is where approvals are kept (root writes, bridges read).
var HostsPath = brand.StateDir + "/plugins-hosts.json"

// ApprovedHost is one approval.
type ApprovedHost struct {
	Plugin string `json:"plugin"`
	// Host is the exact host:port.
	Host string `json:"host"`
	// Scheme is "https" (default) or "http".
	Scheme string    `json:"scheme"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
}

type hostsFile struct {
	Approved []ApprovedHost `json:"approved"`
}

var hostsMu sync.Mutex

func readHosts() hostsFile {
	var f hostsFile
	b, err := os.ReadFile(HostsPath)
	if err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

func writeHosts(f hostsFile) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(HostsPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(HostsPath), ".hosts-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	// World-readable on purpose: every user's bridge reads the approvals
	// (plugins.network.request/list, plugins.access for the frame's
	// policy). The file holds plugin ids, host names and the approver's
	// user name, nothing secret; only root writes it (state dir 0755).
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), HostsPath)
}

// approvedFor lists the approvals of one plugin.
func approvedFor(plugin string) []ApprovedHost {
	var out []ApprovedHost
	for _, a := range readHosts().Approved {
		if a.Plugin == plugin {
			out = append(out, a)
		}
	}
	return out
}

// NormaliseHost turns "Host", "host:443" or "HOST:8443" into the exact
// "host:port" (default port 443 for https, 80 for http). Wildcards,
// schemes, paths and credentials are refused.
func NormaliseHost(raw, scheme string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" || strings.ContainsAny(h, "*/@?# \t") || strings.Contains(h, "://") {
		return "", fmt.Errorf("Give a host name with an optional port, like registry.example.org or registry.example.org:5000.")
	}
	port := "443"
	if scheme == "http" {
		port = "80"
	}
	host := h
	if i := strings.LastIndex(h, ":"); i >= 0 {
		host, port = h[:i], h[i+1:]
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("The port must be a number from 1 to 65535.")
		}
	}
	full := host + ":" + port
	if len(full) > 253 || !NetworkHostRe.MatchString(full) {
		return "", fmt.Errorf("%q is not a valid host name.", raw)
	}
	return full, nil
}

// declaredHost reports whether the manifest's own list covers host:port.
func declaredHost(m *Manifest, hostPort string) bool {
	host := hostPort[:strings.LastIndex(hostPort, ":")]
	port := hostPort[strings.LastIndex(hostPort, ":")+1:]
	for _, d := range m.Capabilities.Network {
		dh, dp := d, "443"
		if i := strings.LastIndex(d, ":"); i >= 0 {
			dh, dp = d[:i], d[i+1:]
		}
		if dp != port {
			continue
		}
		if dh == host || (strings.HasPrefix(dh, "*.") && strings.HasSuffix(host, dh[1:])) {
			return true
		}
	}
	return false
}

type netParams struct {
	Plugin string `json:"plugin"`
	Host   string `json:"host"`
	Scheme string `json:"scheme"`
}

func (p *netParams) scheme() (string, error) {
	switch p.Scheme {
	case "", "https":
		return "https", nil
	case "http":
		return "http", nil
	}
	return "", rpc.Errorf(rpc.Invalid, "The scheme must be https or http.")
}

// NetworkRequest is the result of plugins.network.request.
type NetworkRequest struct {
	// Status is "approved" (listed in the manifest or already approved) or
	// "pending" (an administrator has to approve it).
	Status string `json:"status"`
	Host   string `json:"host"`
	Scheme string `json:"scheme"`
}

func registerEnvs(r *rpc.Registry) {
	r.Handle("plugins.envCheck", rpc.User, envCheck)
	r.Handle("plugins.network.request", rpc.User, netRequest)
	r.Handle("plugins.network.approve", rpc.Admin, netApprove)
	r.Handle("plugins.network.list", rpc.User, netList)
	r.Handle("plugins.network.revoke", rpc.Admin, netRevoke)
}

func netRequest(_ context.Context, c *rpc.Call) (any, error) {
	var p netParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	f, _, err := authorize(c, p.Plugin)
	if err != nil {
		return nil, err
	}
	if !f.M.Capabilities.UserHosts {
		return nil, rpc.Errorf(rpc.Forbidden, `%s does not declare capabilities.network.userHosts, so it may not ask for more hosts.`, f.M.Name)
	}
	sch, err := p.scheme()
	if err != nil {
		return nil, err
	}
	hp, nerr := NormaliseHost(p.Host, sch)
	if nerr != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", nerr)
	}
	if declaredHost(f.M, hp) {
		return NetworkRequest{Status: "approved", Host: hp, Scheme: "https"}, nil
	}
	for _, a := range approvedFor(f.M.ID) {
		// The scheme must match: an approval for plain http is not one for
		// https (or the other way round); the frame's policy holds exactly
		// what was approved.
		if a.Host == hp && a.Scheme == sch {
			return NetworkRequest{Status: "approved", Host: hp, Scheme: a.Scheme}, nil
		}
	}
	return NetworkRequest{Status: "pending", Host: hp, Scheme: sch}, nil
}

func netApprove(_ context.Context, c *rpc.Call) (any, error) {
	var p netParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if !idRe.MatchString(p.Plugin) {
		return nil, rpc.Errorf(rpc.Invalid, "%q is not a plugin id.", p.Plugin)
	}
	f := find(readPolicy(), p.Plugin)
	if f == nil {
		return nil, rpc.Errorf(rpc.NotFound, "There is no plugin %q.", p.Plugin)
	}
	if !f.M.Capabilities.UserHosts {
		return nil, rpc.Errorf(rpc.Forbidden, "%s does not declare capabilities.network.userHosts.", f.M.Name)
	}
	sch, err := p.scheme()
	if err != nil {
		return nil, err
	}
	hp, nerr := NormaliseHost(p.Host, sch)
	if nerr != nil {
		return nil, rpc.Errorf(rpc.Invalid, "%v", nerr)
	}
	hostsMu.Lock()
	defer hostsMu.Unlock()
	file := readHosts()
	by := os.Getenv("SUDO_USER")
	if by == "" {
		by = "root"
	}
	kept := file.Approved[:0]
	for _, a := range file.Approved {
		if !(a.Plugin == p.Plugin && a.Host == hp) {
			kept = append(kept, a)
		}
	}
	file.Approved = append(kept, ApprovedHost{Plugin: p.Plugin, Host: hp, Scheme: sch, By: by, At: time.Now().UTC().Truncate(time.Second)})
	if len(file.Approved) > 512 {
		return nil, rpc.Errorf(rpc.Invalid, "Too many approved hosts. Revoke some first.")
	}
	if err := writeHosts(file); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the approval: %v", cleanErr(err))
	}
	return NetworkRequest{Status: "approved", Host: hp, Scheme: sch}, nil
}

func netList(_ context.Context, c *rpc.Call) (any, error) {
	all := readHosts().Approved
	if all == nil {
		all = []ApprovedHost{}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Plugin != all[j].Plugin {
			return all[i].Plugin < all[j].Plugin
		}
		return all[i].Host < all[j].Host
	})
	return map[string]any{"approved": all}, nil
}

func netRevoke(_ context.Context, c *rpc.Call) (any, error) {
	var p netParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	hostsMu.Lock()
	defer hostsMu.Unlock()
	file := readHosts()
	var kept []ApprovedHost
	found := false
	for _, a := range file.Approved {
		if a.Plugin == p.Plugin && a.Host == strings.ToLower(p.Host) {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return nil, rpc.Errorf(rpc.NotFound, "That host is not approved for %s.", p.Plugin)
	}
	file.Approved = kept
	if err := writeHosts(file); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "Could not save the change: %v", cleanErr(err))
	}
	return map[string]any{"plugin": p.Plugin, "host": p.Host}, nil
}
