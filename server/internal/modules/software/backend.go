package software

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// Kinds of software source. A backend has exactly one kind; the web client
// routes transactions by kind.
const (
	KindRepo    = "repo"
	KindAUR     = "aur"
	KindFlatpak = "flatpak"
)

// Package is an installed package or app.
type Package struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"` // display name (flatpak apps)
	Version     string `json:"version"`
	Source      string `json:"source"` // core, extra, aur, flatpak, apt, dnf...
	Kind        string `json:"kind"`
	Size        int64  `json:"size"`
	Reason      string `json:"reason"`      // explicit | dependency
	InstallDate int64  `json:"installDate"` // unix seconds, 0 = unknown
	Description string `json:"description"`
	Orphan      bool   `json:"orphan"`
	Scope       string `json:"scope,omitempty"` // flatpak: system | user
}

// Update is one pending upgrade.
type Update struct {
	Name   string   `json:"name"`
	Title  string   `json:"title,omitempty"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Source string   `json:"source"`
	Kind   string   `json:"kind"`
	Size   int64    `json:"size"`
	Notes  []string `json:"notes"` // reboot | restartService:<unit> | security
	Scope  string   `json:"scope,omitempty"`
}

// Result is one search hit.
type Result struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	Remote      string `json:"remote,omitempty"` // flatpak remote
}

// Field is one line of a package's detail sheet.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Detail is the answer of software.info.
type Detail struct {
	Name        string  `json:"name"`
	Source      string  `json:"source"`
	Kind        string  `json:"kind"`
	Installed   bool    `json:"installed"`
	Version     string  `json:"version"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

// Progress is what a Step parser learns from the output.
type Progress struct {
	Done    int
	Total   int
	Current string
}

// Step is one command of a transaction.
type Step struct {
	Title string
	Name  string
	Args  []string
	Env   []string
	// Parse inspects one output line and updates p; it reports whether p changed.
	Parse func(line string, p *Progress) bool
}

// Plan is the ordered list of commands a change needs.
type Plan struct{ Steps []Step }

// Backend is one package manager.
type Backend interface {
	Name() string
	Kind() string
	Available() bool
	ListInstalled(ctx context.Context) ([]Package, error)
	ListUpdates(ctx context.Context) ([]Update, error)
	Search(ctx context.Context, query string) ([]Result, error)
	Info(ctx context.Context, name string) (*Detail, error)
	// Install, Remove and Upgrade build the commands; they run in the root bridge.
	// Upgrade with no packages upgrades everything.
	Install(pkgs []string) (Plan, error)
	Remove(pkgs []string) (Plan, error)
	Upgrade(pkgs []string) (Plan, error)
	// Refresh updates the package metadata as far as the caller's rights allow.
	Refresh(ctx context.Context) error
}

// Scoped backends (flatpak) can act on the system or the user installation.
type scoped interface {
	WithScope(scope string) Backend
}

// ownerFinder maps files to the packages that own them.
type ownerFinder interface {
	Owners(ctx context.Context, files []string) map[string]string
}

// lookuper resolves a list of exact package names to search results.
type lookuper interface {
	Lookup(ctx context.Context, names []string) ([]Result, error)
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+:~-]*$`)

// validNames checks every package name; they are passed to commands as argv
// but must still never look like an option.
func validNames(names []string) error {
	if len(names) > 500 {
		return rpc.Errorf(rpc.Invalid, "too many packages in one request (max 500)")
	}
	for _, n := range names {
		if len(n) > 200 || !nameRe.MatchString(n) {
			return rpc.Errorf(rpc.Invalid, "%q is not a valid package name", n)
		}
	}
	return nil
}

// run executes a command and returns stdout. Errors with exit codes listed in
// ok are not errors.
func run(ctx context.Context, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	out, err := sys.Cmd{Name: name, Args: args, Env: env, Timeout: timeout}.Output(ctx)
	return string(out), err
}

func exitCode(err error) int {
	if ee, ok := err.(*sys.ExitError); ok {
		return ee.Code
	}
	return -1
}

func have(name string) bool {
	_, err := sys.LookPath(name)
	return err == nil
}

func isRoot() bool { return os.Geteuid() == 0 }

var sizeRe = regexp.MustCompile(`^([0-9]+(?:[.,][0-9]+)?)\s*([A-Za-z]*)$`)

// parseHumanSize parses "14.1 MB", "608,2 MB", "1 KiB" into bytes.
func parseHumanSize(s string) int64 {
	m := sizeRe.FindStringSubmatch(strings.TrimSpace(strings.ReplaceAll(s, "\u00a0", " ")))
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
	if err != nil {
		return 0
	}
	mult := float64(1)
	switch strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(strings.ToUpper(m[2]), "IB"), "B")) {
	case "K":
		mult = 1 << 10
	case "M":
		mult = 1 << 20
	case "G":
		mult = 1 << 30
	case "T":
		mult = 1 << 40
	}
	return int64(f * mult)
}

// parseKV turns "Key : value" / "Key: value" blocks into ordered fields.
// Continuation lines (leading space) are appended to the previous value.
func parseKV(text string) []Field {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	var out []Field
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			if len(out) > 0 {
				break // only the first stanza
			}
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if len(out) > 0 {
				out[len(out)-1].Value += " " + strings.TrimSpace(line)
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if v == "" || v == "None" || v == "(none)" {
			continue
		}
		out = append(out, Field{k, v})
	}
	return out
}

func fieldValue(f []Field, keys ...string) string {
	for _, k := range keys {
		for _, x := range f {
			if strings.EqualFold(x.Key, k) {
				return x.Value
			}
		}
	}
	return ""
}

var kernelRe = regexp.MustCompile(`^(linux(-(lts|zen|hardened|rt|rt-lts|cachyos[a-z0-9-]*|lqx|xanmod[a-z0-9-]*|mainline|tkg[a-z0-9-]*))?|kernel|kernel-core|kernel-default|linux-image-.+|linux-generic.*|systemd|libsystemd0|systemd-sysv|glibc|libc6)$`)

// needsReboot reports whether upgrading name (a kernel, systemd, glibc) needs a restart.
func needsReboot(name string) bool { return kernelRe.MatchString(name) }

func hasNote(u Update, note string) bool {
	for _, n := range u.Notes {
		if n == note {
			return true
		}
	}
	return false
}

// ttlCache keeps computed values for a while; one mutex per key so parallel
// requests share one computation.
type ttlCache struct {
	mu    sync.Mutex
	items map[string]*cacheItem
}

type cacheItem struct {
	mu  sync.Mutex
	val any
	at  time.Time
}

func (c *ttlCache) item(key string) *cacheItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]*cacheItem{}
	}
	it := c.items[key]
	if it == nil {
		it = &cacheItem{}
		c.items[key] = it
	}
	return it
}

// get returns the cached value (age under ttl) or computes it. force skips the cache.
func (c *ttlCache) get(key string, ttl time.Duration, force bool, fn func() (any, error)) (any, time.Time, error) {
	it := c.item(key)
	it.mu.Lock()
	defer it.mu.Unlock()
	if !force && it.val != nil && time.Since(it.at) < ttl {
		return it.val, it.at, nil
	}
	v, err := fn()
	if err != nil {
		if it.val != nil {
			return it.val, it.at, nil // serve stale data rather than nothing
		}
		return nil, time.Time{}, err
	}
	it.val, it.at = v, time.Now()
	return v, it.at, nil
}

func (c *ttlCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, it := range c.items {
		it.mu.Lock()
		it.val = nil
		it.mu.Unlock()
	}
}

// when returns the time a key was last computed (zero if never).
func (c *ttlCache) when(key string) time.Time {
	it := c.item(key)
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.val == nil {
		return time.Time{}
	}
	return it.at
}
