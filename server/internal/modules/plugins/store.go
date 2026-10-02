package plugins

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ervisio/ervisio/server/internal/brand"
	cfg "github.com/ervisio/ervisio/server/internal/config"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
)

// Locations a plugin can be found in.
const (
	LocSystem    = "system"    // /usr/share/ervisio/plugins (packaged)
	LocInstalled = "installed" // /var/lib/ervisio/plugins (from Browse)
	LocDev       = "dev"       // ./plugins of the repo, or a folder loaded with plugins.loadDev
)

// Paths and trust settings. Variables so tests can point them at a temp dir.
var (
	SystemDir      = brand.PackagedPluginsDir
	InstalledDir   = brand.InstalledPluginsDir
	StatePath      = brand.StateDir + "/plugins-state.json"
	CatalogURLFile = brand.ConfigDir + "/plugins-catalog.url"
	// DevDirs overrides the dev folder list (tests).
	DevDirs []string
	// DaemonDev is set by the bridge's --dev flag: the daemon runs in
	// --dev, so plugins.loadDev is allowed without plugins.dev.
	DaemonDev bool
	// DaemonPluginsDir is the daemon's ./plugins folder in --dev (bridge
	// flag --dev-plugins), scanned as a dev location.
	DaemonPluginsDir string
	// Keys are the trusted signing keys.
	Keys = func() []ed25519.PublicKey { return TrustedKeys }
)

// Found is a plugin folder discovered on disk.
type Found struct {
	M        *Manifest
	Dir      string
	Location string
	Sig      Signature
	// Err is set when the folder holds a plugin that failed validation.
	Err string
	// Name is the folder name (the plugin id when valid).
	Name string
}

// policy is what the daemon configuration says about plugins.
type policy struct {
	AllowUnsigned bool
	Dev           bool
}

func readPolicy() policy {
	c, _, _, err := cfg.Load(configmod.Path)
	if err != nil || c == nil {
		d := cfg.Default()
		return policy{AllowUnsigned: d.Plugins.AllowUnsigned, Dev: d.Plugins.Dev}
	}
	return policy{AllowUnsigned: c.Plugins.AllowUnsigned, Dev: c.Plugins.Dev}
}

// devDirs returns the folders scanned as "dev" plugin locations: the
// daemon's ./plugins when it runs in --dev (passed explicitly to the bridge).
func devDirs(p policy) []string {
	if DevDirs != nil {
		return DevDirs
	}
	if DaemonDev && DaemonPluginsDir != "" && filepath.IsAbs(DaemonPluginsDir) {
		return []string{filepath.Clean(DaemonPluginsDir)}
	}
	return nil
}

// devEnabled says whether plugins.loadDev is allowed: plugins.dev = true in
// the configuration, or a daemon in --dev.
func devEnabled(p policy) bool { return p.Dev || DevDirs != nil || DaemonDev }

// scan lists every plugin folder, first location wins on duplicate ids.
func scan(p policy) []*Found {
	type root struct {
		dir, loc string
		single   bool // dir is itself a plugin folder (registered with loadDev)
	}
	var roots []root
	for _, d := range devDirs(p) {
		roots = append(roots, root{dir: d, loc: LocDev})
	}
	if devEnabled(p) {
		for _, d := range registeredDev() {
			roots = append(roots, root{dir: d, loc: LocDev, single: true})
		}
	}
	roots = append(roots, root{dir: SystemDir, loc: LocSystem}, root{dir: InstalledDir, loc: LocInstalled})

	var out []*Found
	seen := map[string]bool{}
	add := func(dir, loc string) {
		name := filepath.Base(dir)
		fi, err := os.Stat(dir) // follows symlinks: dev folders may be symlinked
		if err != nil || !fi.IsDir() {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
			return
		}
		f := &Found{Dir: dir, Location: loc, Name: name}
		m, err := LoadManifest(dir)
		switch {
		case err != nil:
			f.Err = err.Error()
		case m.ID != name && loc != LocDev:
			f.Err = fmt.Sprintf("the folder is named %q but the manifest id is %q", name, m.ID)
		default:
			f.M = m
			f.Sig = CheckSignature(dir, Keys())
		}
		key := name
		if f.M != nil {
			key = f.M.ID
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, f)
	}
	for _, r := range roots {
		if r.single {
			add(r.dir, r.loc)
			continue
		}
		ents, err := os.ReadDir(r.dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			add(filepath.Join(r.dir, e.Name()), r.loc)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func find(p policy, id string) *Found {
	for _, f := range scan(p) {
		if f.M != nil && f.M.ID == id {
			return f
		}
	}
	return nil
}

// ---- enabled state ----

type state struct {
	Enabled map[string]bool `json:"enabled"`
}

var stateMu sync.Mutex

func readState() state {
	var s state
	if b, err := os.ReadFile(StatePath); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Enabled == nil {
		s.Enabled = map[string]bool{}
	}
	return s
}

func (s state) isEnabled(id string) bool {
	v, ok := s.Enabled[id]
	return !ok || v
}

func writeState(s state) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(StatePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(StatePath, append(b, '\n'), 0o644)
}

func writeFileAtomic(p string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// ---- who is calling ----

type caller struct {
	Name   string
	Groups map[string]bool
	Admin  bool
}

func currentCaller(adminBridge bool) caller {
	c := caller{Groups: map[string]bool{}, Admin: adminBridge || os.Geteuid() == 0}
	u, err := user.Current()
	if err != nil {
		return c
	}
	c.Name = u.Username
	ids, _ := u.GroupIds()
	for _, id := range ids {
		if g, err := user.LookupGroupId(id); err == nil {
			c.Groups[g.Name] = true
		}
	}
	for _, g := range []string{"sudo", "wheel", "admin"} {
		if c.Groups[g] {
			c.Admin = true // can unlock administrator rights
		}
	}
	return c
}

func (c caller) canSee(m *Manifest) bool {
	if c.Admin || len(m.VisibleTo.Groups) == 0 {
		return true
	}
	for _, g := range m.VisibleTo.Groups {
		if c.Groups[g] {
			return true
		}
	}
	return false
}

// ---- list ----

// Info is one entry of plugins.list.
type Info struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Version        string       `json:"version"`
	Author         string       `json:"author"`
	Description    string       `json:"description"`
	Icon           string       `json:"icon"`
	Color          string       `json:"color"`
	Entry          string       `json:"entry"`
	Enabled        bool         `json:"enabled"`
	Signed         bool         `json:"signed"`
	Verified       bool         `json:"verified"`
	SignatureError string       `json:"signatureError,omitempty"`
	Capabilities   Capabilities `json:"capabilities"`
	Contributes    Contributes  `json:"contributes"`
	VisibleTo      VisibleTo    `json:"visibleTo"`
	Location       string       `json:"location"`
	Removable      bool         `json:"removable"`
	Dir            string       `json:"dir,omitempty"`
	// Unloadable: a dev plugin that was loaded with plugins.loadDev.
	Unloadable      bool        `json:"unloadable,omitempty"`
	UpdateAvailable *UpdateInfo `json:"updateAvailable,omitempty"`
	// Blocked: unsigned or invalid plugin while plugins.allow_unsigned is off.
	Blocked bool `json:"blocked,omitempty"`
	// DevUnsigned: a dev-folder plugin without a valid signature that runs
	// only because developer mode is on (shown with an "Unsigned, dev" badge).
	DevUnsigned bool `json:"devUnsigned,omitempty"`
	// Error: the folder holds a plugin that could not be loaded.
	Error string `json:"error,omitempty"`
}

// UpdateInfo says a newer version is in the catalog.
type UpdateInfo struct {
	Version        string `json:"version"`
	Notes          string `json:"notes,omitempty"`
	NewPermissions bool   `json:"newPermissions"`
	Source         string `json:"source,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
}

func list(adminBridge bool) []Info {
	p := readPolicy()
	who := currentCaller(adminBridge)
	st := readState()
	cat, _ := loadLocalCatalog()
	out := []Info{}
	for _, f := range scan(p) {
		if f.M == nil {
			if who.Admin {
				out = append(out, Info{ID: f.Name, Name: f.Name, Location: f.Location, Dir: f.Dir, Error: f.Err, Capabilities: emptyCaps(), Contributes: emptyContrib(), VisibleTo: VisibleTo{Groups: []string{}}})
			}
			continue
		}
		m := f.M
		if !who.canSee(m) {
			continue
		}
		in := Info{
			ID: m.ID, Name: m.Name, Version: m.Version, Author: m.Author, Description: m.Description,
			Icon: m.Icon, Color: m.Color, Entry: m.Entry,
			Enabled: st.isEnabled(m.ID), Signed: f.Sig.Signed, Verified: f.Sig.Verified, SignatureError: f.Sig.Err,
			Capabilities: m.Capabilities, Contributes: m.Contributes, VisibleTo: m.VisibleTo,
			Location: f.Location, Removable: f.Location == LocInstalled, Dir: f.Dir, Unloadable: f.Location == LocDev && isLoadedDev(f.Dir),
		}
		if in.Icon == "" {
			in.Icon = "plugins"
		}
		if in.Color == "" {
			in.Color = "plg"
		}
		switch trust(p, f) {
		case trustBlocked:
			in.Blocked = true
			in.Enabled = false
		case trustDevUnsigned:
			in.DevUnsigned = true
		}
		if e := cat.find(m.ID); e != nil && compareSemver(e.Version, m.Version) > 0 && f.Location != LocDev {
			in.UpdateAvailable = &UpdateInfo{Version: e.Version, Notes: e.Notes, NewPermissions: !sameJSON(e.Capabilities, m.Capabilities), Source: e.Source, SHA256: e.SHA256}
		}
		out = append(out, in)
	}
	return out
}

func emptyCaps() Capabilities {
	m := Manifest{}
	m.normalise()
	return m.Capabilities
}
func emptyContrib() Contributes {
	m := Manifest{}
	m.normalise()
	return m.Contributes
}

// compareSemver compares two versions: -1, 0, 1. Pre-releases sort before the release.
func compareSemver(a, b string) int {
	pa, pre1 := splitVer(a)
	pb, pre2 := splitVer(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case pre1 == pre2:
		return 0
	case pre1 == "":
		return 1
	case pre2 == "":
		return -1
	case pre1 < pre2:
		return -1
	}
	return 1
}

func splitVer(v string) ([3]int, string) {
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	var n [3]int
	for i, s := range strings.SplitN(core, ".", 3) {
		n[i], _ = strconv.Atoi(s)
	}
	return n, pre
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// isLoadedDev reports whether dir was registered with plugins.loadDev (directly or through a link).
func isLoadedDev(dir string) bool {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	for _, f := range registeredDev() {
		if f == real || f == dir {
			return true
		}
	}
	return false
}
