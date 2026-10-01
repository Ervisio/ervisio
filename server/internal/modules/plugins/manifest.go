package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Limits applied to manifests.
const (
	maxManifestBytes = 256 << 10
	maxCommands      = 64
	maxArgs          = 16
	maxListItems     = 64
)

var (
	idRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	semverRe  = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
	cmdNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)
	iconRe    = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	groupRe   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	slotRe    = regexp.MustCompile(`\{(\d+)\}`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hues      = map[string]bool{"ov": true, "term": true, "file": true, "log": true, "svc": true, "sw": true, "usr": true, "plg": true}
)

// Manifest is manifest.json. Unknown fields are rejected.
type Manifest struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Author       string            `json:"author,omitempty"`
	Description  string            `json:"description,omitempty"`
	Homepage     string            `json:"homepage,omitempty"`
	Icon         string            `json:"icon,omitempty"`
	Color        string            `json:"color,omitempty"`
	Entry        string            `json:"entry"`
	Files        map[string]string `json:"files,omitempty"` // path -> sha256 hex; required for signed plugins
	Capabilities Capabilities      `json:"capabilities"`
	Contributes  Contributes       `json:"contributes"`
	VisibleTo    VisibleTo         `json:"visibleTo"`
}

// Capabilities is everything a plugin may ask the host to do.
type Capabilities struct {
	Commands []Command  `json:"commands"`
	Files    FileAccess `json:"files"`
	Sockets  []string   `json:"sockets"`
	Network  []string   `json:"network"`
}

// FileAccess lists folders the plugin reads or edits.
type FileAccess struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
}

// Command is one argv the plugin may run through plugins.exec.
type Command struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Argv        []string  `json:"argv"`
	Args        []ArgSpec `json:"args,omitempty"`
	// Admin means the command needs the root bridge.
	Admin bool `json:"admin"`
	// AdminUnlessGroup makes an admin command run as the user when the user is in this group.
	AdminUnlessGroup string `json:"adminUnlessGroup,omitempty"`
	// TimeoutSec bounds plugins.exec (default 30, max 600).
	TimeoutSec int `json:"timeoutSec,omitempty"`
}

// ArgSpec constrains one {N} slot of argv.
type ArgSpec struct {
	// Pattern must match the whole argument.
	Pattern string `json:"pattern"`
	// AllowDash permits values starting with "-" (default: refused, so a value cannot become an option).
	AllowDash bool `json:"allowDash,omitempty"`
	MaxLen    int  `json:"maxLen,omitempty"`
	re        *regexp.Regexp
}

// Contributes lists what the plugin adds to the interface.
type Contributes struct {
	Pages    []Contribution `json:"pages"`
	Widgets  []Contribution `json:"widgets"`
	Snippets []Snippet      `json:"snippets"`
}

// Contribution is a page or widget.
type Contribution struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
}

// Snippet is a terminal snippet.
type Snippet struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// VisibleTo limits who sees the plugin; empty = everyone.
type VisibleTo struct {
	Groups []string `json:"groups"`
}

// ParseManifest decodes and validates manifest.json bytes (without touching the disk).
func ParseManifest(data []byte) (*Manifest, error) {
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("manifest.json is larger than %d KiB", maxManifestBytes>>10)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest.json: %v", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("manifest.json: trailing data after the manifest")
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	m.normalise()
	return &m, nil
}

// normalise makes nil slices empty so JSON output has [] instead of null.
func (m *Manifest) normalise() {
	c := &m.Capabilities
	if c.Commands == nil {
		c.Commands = []Command{}
	}
	if c.Files.Read == nil {
		c.Files.Read = []string{}
	}
	if c.Files.Write == nil {
		c.Files.Write = []string{}
	}
	if c.Sockets == nil {
		c.Sockets = []string{}
	}
	if c.Network == nil {
		c.Network = []string{}
	}
	if m.Contributes.Pages == nil {
		m.Contributes.Pages = []Contribution{}
	}
	if m.Contributes.Widgets == nil {
		m.Contributes.Widgets = []Contribution{}
	}
	if m.Contributes.Snippets == nil {
		m.Contributes.Snippets = []Snippet{}
	}
	if m.VisibleTo.Groups == nil {
		m.VisibleTo.Groups = []string{}
	}
	for i := range c.Commands {
		if c.Commands[i].Argv == nil {
			c.Commands[i].Argv = []string{}
		}
	}
}

// LoadManifest reads and fully validates the plugin in dir: manifest syntax,
// and that the entry file exists, is a regular file and stays inside dir.
func LoadManifest(dir string) (*Manifest, error) {
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("manifest.json: %v", cleanErr(err))
	}
	defer f.Close()
	buf := make([]byte, maxManifestBytes+1)
	n, _ := readFull(f, buf)
	m, err := ParseManifest(buf[:n])
	if err != nil {
		return nil, err
	}
	if err := checkInside(dir, m.Entry, "entry"); err != nil {
		return nil, err
	}
	for p := range m.Files {
		if err := checkInside(dir, p, "files"); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func readFull(f *os.File, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := f.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func cleanErr(err error) string {
	if pe, ok := err.(*fs.PathError); ok {
		return pe.Err.Error()
	}
	return err.Error()
}

// checkInside verifies that rel names an existing regular file inside dir,
// also after resolving symlinks.
func checkInside(dir, rel, what string) error {
	if !validRelPath(rel) {
		return fmt.Errorf("%s %q must be a relative path inside the plugin folder", what, rel)
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("plugin folder: %v", cleanErr(err))
	}
	realFile, err := filepath.EvalSymlinks(full)
	if err != nil {
		return fmt.Errorf("%s %q does not exist", what, rel)
	}
	r, err := filepath.Rel(realDir, realFile)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return fmt.Errorf("%s %q points outside the plugin folder", what, rel)
	}
	fi, err := os.Stat(realFile)
	if err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%s %q is not a regular file", what, rel)
	}
	return nil
}

func validRelPath(p string) bool {
	if p == "" || len(p) > 256 || strings.ContainsAny(p, "\\\x00") || strings.HasPrefix(p, "/") {
		return false
	}
	if path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return false
	}
	return true
}

func (m *Manifest) validate() error {
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("id %q is invalid: use 2-40 lowercase letters, digits or dashes, starting with a letter", m.ID)
	}
	if n := strings.TrimSpace(m.Name); n == "" || len(m.Name) > 60 {
		return fmt.Errorf("name is required and at most 60 characters")
	}
	if !semverRe.MatchString(m.Version) {
		return fmt.Errorf("version %q is not a semantic version (like 1.4.0)", m.Version)
	}
	if len(m.Author) > 80 || len(m.Description) > 300 || len(m.Homepage) > 200 {
		return fmt.Errorf("author, description or homepage is too long")
	}
	if m.Icon != "" && !iconRe.MatchString(m.Icon) {
		return fmt.Errorf("icon %q is invalid", m.Icon)
	}
	if m.Color != "" && !hues[m.Color] {
		return fmt.Errorf("color %q must be one of ov, term, file, log, svc, sw, usr, plg", m.Color)
	}
	if !validRelPath(m.Entry) || !(strings.HasSuffix(m.Entry, ".js") || strings.HasSuffix(m.Entry, ".mjs")) {
		return fmt.Errorf("entry %q must be a relative .js or .mjs path inside the plugin folder", m.Entry)
	}
	for p, h := range m.Files {
		if !validRelPath(p) || p == "manifest.json" || p == "manifest.sig" {
			return fmt.Errorf("files: %q is not a valid file path", p)
		}
		if !shaRe.MatchString(h) {
			return fmt.Errorf("files: %q needs a lowercase hex sha256", p)
		}
	}
	if len(m.Files) > 0 {
		if _, ok := m.Files[m.Entry]; !ok {
			return fmt.Errorf("files must list the entry file %q", m.Entry)
		}
	}
	if err := m.Capabilities.validate(); err != nil {
		return err
	}
	if err := m.Contributes.validate(); err != nil {
		return err
	}
	if len(m.VisibleTo.Groups) > maxListItems {
		return fmt.Errorf("visibleTo.groups has too many entries")
	}
	for _, g := range m.VisibleTo.Groups {
		if !groupRe.MatchString(g) {
			return fmt.Errorf("visibleTo: %q is not a valid group name", g)
		}
	}
	return nil
}

func validAbs(p string, allowHome bool) bool {
	if len(p) == 0 || len(p) > 512 || strings.ContainsAny(p, "\x00\n") {
		return false
	}
	if allowHome && (p == "~" || strings.HasPrefix(p, "~/")) {
		return !strings.Contains(p, "..")
	}
	return path.IsAbs(p) && path.Clean(p) == p
}

func (c *Capabilities) validate() error {
	if len(c.Commands) > maxCommands {
		return fmt.Errorf("capabilities.commands: at most %d commands", maxCommands)
	}
	seen := map[string]bool{}
	for i := range c.Commands {
		cmd := &c.Commands[i]
		if err := cmd.validate(); err != nil {
			return fmt.Errorf("command %q: %v", cmd.Name, err)
		}
		if seen[cmd.Name] {
			return fmt.Errorf("command %q is declared twice", cmd.Name)
		}
		seen[cmd.Name] = true
	}
	for _, l := range []struct {
		what string
		list []string
		home bool
	}{{"files.read", c.Files.Read, true}, {"files.write", c.Files.Write, true}, {"sockets", c.Sockets, false}} {
		if len(l.list) > maxListItems {
			return fmt.Errorf("capabilities.%s has too many entries", l.what)
		}
		for _, p := range l.list {
			if !validAbs(p, l.home) {
				return fmt.Errorf("capabilities.%s: %q must be a clean absolute path", l.what, p)
			}
		}
	}
	if len(c.Network) > maxListItems {
		return fmt.Errorf("capabilities.network has too many entries")
	}
	for _, h := range c.Network {
		if h == "" || len(h) > 253 || strings.ContainsAny(h, " \t\n/\\") {
			return fmt.Errorf("capabilities.network: %q is not a host name", h)
		}
	}
	return nil
}

func (c *Command) validate() error {
	if !cmdNameRe.MatchString(c.Name) {
		return fmt.Errorf("name must be letters, digits, _ or - (max 32)")
	}
	if len(c.Argv) == 0 || len(c.Argv) > 64 {
		return fmt.Errorf("argv must hold between 1 and 64 items")
	}
	if c.Argv[0] == "" || slotRe.MatchString(c.Argv[0]) {
		return fmt.Errorf("the program (argv[0]) must be fixed, not a {N} slot")
	}
	if strings.Contains(c.Argv[0], "/") && !path.IsAbs(c.Argv[0]) {
		return fmt.Errorf("argv[0] must be a bare command name or an absolute path")
	}
	for _, a := range c.Argv {
		if strings.ContainsRune(a, 0) || len(a) > 1024 {
			return fmt.Errorf("argv holds an invalid item")
		}
	}
	if len(c.Args) > maxArgs {
		return fmt.Errorf("at most %d args", maxArgs)
	}
	used := map[int]bool{}
	for _, a := range c.Argv[1:] {
		for _, mm := range slotRe.FindAllStringSubmatch(a, -1) {
			n, err := strconv.Atoi(mm[1])
			if err != nil || n >= len(c.Args) {
				return fmt.Errorf("argv uses slot {%s} but args declares only %d", mm[1], len(c.Args))
			}
			used[n] = true
		}
	}
	for i := range c.Args {
		if !used[i] {
			return fmt.Errorf("args[%d] is declared but argv never uses {%d}", i, i)
		}
		p := c.Args[i].Pattern
		if p == "" || len(p) > 256 {
			return fmt.Errorf("args[%d] needs a pattern (a regular expression the whole value must match)", i)
		}
		re, err := regexp.Compile(`^(?:` + p + `)$`)
		if err != nil {
			return fmt.Errorf("args[%d] pattern: %v", i, err)
		}
		c.Args[i].re = re
		if c.Args[i].MaxLen < 0 || c.Args[i].MaxLen > 4096 {
			return fmt.Errorf("args[%d] maxLen is out of range", i)
		}
	}
	if c.AdminUnlessGroup != "" {
		if !c.Admin {
			return fmt.Errorf("adminUnlessGroup only makes sense with admin: true")
		}
		if !groupRe.MatchString(c.AdminUnlessGroup) {
			return fmt.Errorf("adminUnlessGroup %q is not a group name", c.AdminUnlessGroup)
		}
	}
	if c.TimeoutSec < 0 || c.TimeoutSec > 600 {
		return fmt.Errorf("timeoutSec must be between 0 and 600")
	}
	return nil
}

func (c *Contributes) validate() error {
	for _, l := range []struct {
		what string
		list []Contribution
	}{{"pages", c.Pages}, {"widgets", c.Widgets}} {
		if len(l.list) > maxListItems {
			return fmt.Errorf("contributes.%s has too many entries", l.what)
		}
		seen := map[string]bool{}
		for _, p := range l.list {
			if !cmdNameRe.MatchString(p.ID) || strings.TrimSpace(p.Title) == "" || len(p.Title) > 60 {
				return fmt.Errorf("contributes.%s: id %q or its title is invalid", l.what, p.ID)
			}
			if p.Icon != "" && !iconRe.MatchString(p.Icon) {
				return fmt.Errorf("contributes.%s: icon %q is invalid", l.what, p.Icon)
			}
			if seen[p.ID] {
				return fmt.Errorf("contributes.%s: id %q is used twice", l.what, p.ID)
			}
			seen[p.ID] = true
		}
	}
	if len(c.Snippets) > maxListItems {
		return fmt.Errorf("contributes.snippets has too many entries")
	}
	for _, s := range c.Snippets {
		if strings.TrimSpace(s.Name) == "" || len(s.Name) > 60 || s.Command == "" || len(s.Command) > 512 {
			return fmt.Errorf("contributes.snippets: %q needs a name and a command", s.Name)
		}
	}
	return nil
}

// RunsRoot reports whether any command may run as root.
func (m *Manifest) RunsRoot() bool {
	for _, c := range m.Capabilities.Commands {
		if c.Admin {
			return true
		}
	}
	return false
}

func compileSpec(p string) (*regexp.Regexp, error) {
	return regexp.Compile(`^(?:` + p + `)$`)
}
