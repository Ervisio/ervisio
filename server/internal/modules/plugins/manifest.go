package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// Limits applied to manifests.
const (
	maxManifestBytes = 256 << 10
	maxCommands      = 64
	maxArgs          = 16
	maxListItems     = 64
	maxHTTPAPIs      = 16
	maxHTTPRules     = 256
	maxHTTPHeaders   = 32
	maxHTTPBodyLimit = 64 << 20
	// maxHTTPUploadLimit is the largest maxUpload a manifest may ask for.
	maxHTTPUploadLimit = int64(1) << 40
)

var (
	idRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)
	semverRe  = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
	cmdNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,31}$`)
	iconRe    = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	groupRe   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	slotRe    = regexp.MustCompile(`\{(\d+)\}`)
	// httpNameRe is the name of a capabilities.http entry.
	httpNameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	headerNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)
	httpMethods  = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "OPTIONS": true}
	shaRe        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hues         = map[string]bool{"ov": true, "term": true, "file": true, "log": true, "svc": true, "sw": true, "usr": true, "plg": true}
)

// Manifest is manifest.json. Unknown fields are rejected.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Entry       string `json:"entry"`
	// MinCore and Requires say which Ervisio the plugin needs (corecompat.go).
	MinCore  string   `json:"minCore,omitempty"`
	Requires Requires `json:"requires,omitempty"`
	// Platforms: "linux" and/or "windows"; empty = Linux only (platform.go).
	Platforms    []string          `json:"platforms,omitempty"`
	Files        map[string]string `json:"files,omitempty"` // path -> sha256 hex; required for signed plugins
	Capabilities Capabilities      `json:"capabilities"`
	Contributes  Contributes       `json:"contributes"`
	VisibleTo    VisibleTo         `json:"visibleTo"`
	// declared is Capabilities as written, with the entries for every
	// system; Capabilities keeps only this system's (platform.go).
	declared Capabilities
}

// Capabilities is everything a plugin may ask the host to do.
type Capabilities struct {
	// plats are the plugin's platforms, set by Manifest.validate so the
	// entries are checked for the systems they run on.
	plats    []string
	Commands []Command `json:"commands"`
	// HTTP lists the HTTP APIs on unix sockets the plugin may call (SDK v3).
	HTTP    []HTTPAPI  `json:"http"`
	Files   FileAccess `json:"files"`
	Sockets []string   `json:"sockets"`
	// Network lists the hosts the plugin's frame may connect to. In the
	// manifest it is a list, or {"hosts": [...], "userHosts": true} (see
	// netcap.go).
	Network []string `json:"network"`
	// UserHosts lets the plugin ask an administrator to approve more hosts.
	UserHosts bool `json:"userHosts,omitempty"`
	// Jobs are named sequences of the declared commands and HTTP calls
	// the daemon runs in the background (see manifest_jobs.go).
	Jobs []JobDef `json:"jobs,omitempty"`
	// Notify lets the plugin send notifications through plugins.notify
	// and job notify steps.
	Notify bool `json:"notify,omitempty"`
}

// FileAccess lists folders the plugin reads or edits.
type FileAccess struct {
	Read  []Folder `json:"read"`
	Write []Folder `json:"write"`
}

// Folder is one capabilities.files entry. In the manifest it is either a
// path string (SDK v2) or an object {path, admin, adminUnlessGroup, create}.
type Folder struct {
	Path string `json:"path"`
	// Admin folders are accessed on the root bridge, unless the user is in
	// AdminUnlessGroup.
	Admin            bool   `json:"admin,omitempty"`
	AdminUnlessGroup string `json:"adminUnlessGroup,omitempty"`
	// Create makes the daemon create the folder when a write targets it.
	Create bool `json:"create,omitempty"`
	// Platforms limits the entry to some systems (platform.go); empty = all
	// the plugin's platforms.
	Platforms []string `json:"platforms,omitempty"`
}

// plain reports whether the entry is a bare path (written as a string).
func (f Folder) plain() bool {
	return !f.Admin && f.AdminUnlessGroup == "" && !f.Create && len(f.Platforms) == 0
}

// UnmarshalJSON accepts a path string or a strict object.
func (f *Folder) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		*f = Folder{}
		return json.Unmarshal(b, &f.Path)
	}
	type folder Folder
	var v folder
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("a files entry is a path or {path, admin, adminUnlessGroup, create, platforms}: %v", err)
	}
	*f = Folder(v)
	return nil
}

// MarshalJSON writes plain entries back as strings, so v2 manifests and
// consents keep their shape.
func (f Folder) MarshalJSON() ([]byte, error) {
	if f.plain() {
		return json.Marshal(f.Path)
	}
	type folder Folder
	return json.Marshal(folder(f))
}

// HTTPAPI is one capabilities.http entry: an HTTP API on a unix socket,
// reachable through plugins.http / plugins.httpStream for the declared
// rules only.
type HTTPAPI struct {
	Name   string `json:"name"`
	Socket string `json:"socket"`
	// Admin means the socket is reached from the root bridge.
	Admin            bool   `json:"admin"`
	AdminUnlessGroup string `json:"adminUnlessGroup,omitempty"`
	// Headers are the only request headers the plugin may set.
	Headers []string   `json:"headers"`
	Rules   []HTTPRule `json:"rules"`
	// MaxBody caps request and response bodies (default 8 MiB, max 64 MiB).
	MaxBody int64 `json:"maxBody,omitempty"`
	// MaxUpload caps the size of a file sent with plugins.upload (default
	// 20 GiB, max 1 TiB). Uploads and downloads are streamed, so it does not
	// depend on MaxBody.
	MaxUpload int64 `json:"maxUpload,omitempty"`
	// TimeoutSec bounds plugins.http, and the wait for the response
	// headers of plugins.httpStream (default 30, max 600).
	TimeoutSec int `json:"timeoutSec,omitempty"`
	// Remote ("docker") lets calls target an environment instead of Socket.
	Remote string `json:"remote,omitempty"`
	// Platforms limits the entry to some systems; empty = all the plugin's.
	// On Windows Socket may be a named pipe (\\.\pipe\name).
	Platforms []string `json:"platforms,omitempty"`
}

// HTTPRule allows Methods on the URL paths matching Path (a regexp that
// must match the whole decoded path).
type HTTPRule struct {
	Methods []string `json:"methods"`
	Path    string   `json:"path"`
	re      *regexp.Regexp
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
	// PTY commands run only through plugins.pty, in a pseudo-terminal.
	PTY bool `json:"pty,omitempty"`
	// Remote ("docker") lets the command run against an environment: argv
	// holds one {env} item, replaced by the endpoint's address.
	Remote string `json:"remote,omitempty"`
	// Platforms limits the entry to some systems; empty = all the plugin's.
	// Two entries may share a name when their platforms do not overlap.
	Platforms []string `json:"platforms,omitempty"`
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
	m.declared = m.Capabilities
	m.Capabilities = m.Capabilities.forHost()
	return &m, nil
}

// normalise makes nil slices empty so JSON output has [] instead of null.
func (m *Manifest) normalise() {
	c := &m.Capabilities
	if c.Commands == nil {
		c.Commands = []Command{}
	}
	if c.HTTP == nil {
		c.HTTP = []HTTPAPI{}
	}
	for i := range c.HTTP {
		if c.HTTP[i].Headers == nil {
			c.HTTP[i].Headers = []string{}
		}
		if c.HTTP[i].Rules == nil {
			c.HTTP[i].Rules = []HTTPRule{}
		}
	}
	if c.Files.Read == nil {
		c.Files.Read = []Folder{}
	}
	if c.Files.Write == nil {
		c.Files.Write = []Folder{}
	}
	if c.Sockets == nil {
		c.Sockets = []string{}
	}
	if c.Network == nil {
		c.Network = []string{}
	}
	c.normaliseJobs()
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
	if err := validatePlatforms(m.Platforms); err != nil {
		return err
	}
	if err := m.validateCoreReq(); err != nil {
		return err
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
	m.Capabilities.plats = effectivePlatforms(m.Platforms)
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

// validAbsOn is validAbs for every system in on: a Linux path must be a
// clean /path, a Windows one may also be C:\dir.
func validAbsOn(p string, allowHome bool, on []string) bool {
	for _, o := range on {
		if !validAbsFor(p, allowHome, o == "windows") {
			return false
		}
	}
	return true
}

func validAbs(p string, allowHome bool) bool {
	return validAbsFor(p, allowHome, runtime.GOOS == "windows")
}

func validAbsFor(p string, allowHome, windows bool) bool {
	if len(p) == 0 || len(p) > 512 || strings.ContainsAny(p, "\x00\n") {
		return false
	}
	if allowHome && (p == "~" || strings.HasPrefix(p, "~/")) {
		return !strings.Contains(p, "..")
	}
	if windows && winDriveRe.MatchString(p) {
		return !strings.Contains(p, "/") && !hasDotDot(p) && !strings.Contains(p, `\\`) && (len(p) == 3 || !strings.HasSuffix(p, `\`))
	}
	return path.IsAbs(p) && path.Clean(p) == p
}

func (c *Capabilities) validate() error {
	if len(c.Commands) > maxCommands {
		return fmt.Errorf("capabilities.commands: at most %d commands", maxCommands)
	}
	seen := map[string][][]string{}
	for i := range c.Commands {
		cmd := &c.Commands[i]
		if err := c.entryPlatforms(cmd.Platforms); err != nil {
			return fmt.Errorf("command %q: %v", cmd.Name, err)
		}
		if err := cmd.validate(); err != nil {
			return fmt.Errorf("command %q: %v", cmd.Name, err)
		}
		if c.clash(seen, cmd.Name, cmd.Platforms) {
			return fmt.Errorf("command %q is declared twice for the same system", cmd.Name)
		}
	}
	if len(c.HTTP) > maxHTTPAPIs {
		return fmt.Errorf("capabilities.http: at most %d entries", maxHTTPAPIs)
	}
	seen = map[string][][]string{}
	for i := range c.HTTP {
		h := &c.HTTP[i]
		if err := c.entryPlatforms(h.Platforms); err != nil {
			return fmt.Errorf("http %q: %v", h.Name, err)
		}
		if err := h.validate(h.Platforms); err != nil {
			return fmt.Errorf("http %q: %v", h.Name, err)
		}
		if c.clash(seen, h.Name, h.Platforms) {
			return fmt.Errorf("http %q is declared twice for the same system", h.Name)
		}
	}
	for _, l := range []struct {
		what string
		list []Folder
	}{{"files.read", c.Files.Read}, {"files.write", c.Files.Write}} {
		if len(l.list) > maxListItems {
			return fmt.Errorf("capabilities.%s has too many entries", l.what)
		}
		for _, f := range l.list {
			if err := c.entryPlatforms(f.Platforms); err != nil {
				return fmt.Errorf("capabilities.%s %q: %v", l.what, f.Path, err)
			}
			if err := f.validate(checkOn(f.Platforms)); err != nil {
				return fmt.Errorf("capabilities.%s: %v", l.what, err)
			}
		}
	}
	if len(c.Sockets) > maxListItems {
		return fmt.Errorf("capabilities.sockets has too many entries")
	}
	for _, p := range c.Sockets {
		if !validAbs(p, false) {
			return fmt.Errorf("capabilities.sockets: %q must be a clean absolute path", p)
		}
	}
	if len(c.Network) > maxListItems {
		return fmt.Errorf("capabilities.network has too many entries")
	}
	for _, h := range c.Network {
		if len(h) > 253 || !NetworkHostRe.MatchString(h) {
			return fmt.Errorf("capabilities.network: %q is not a host name (like api.example.org, *.example.org or host:8443)", h)
		}
	}
	return c.validateJobs()
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
	return c.validateRemote()
}

func (f Folder) validate(on []string) error {
	if !validAbsOn(f.Path, true, on) {
		return fmt.Errorf("%q must be a clean absolute path (or one starting with ~/)", f.Path)
	}
	if f.AdminUnlessGroup != "" {
		if !f.Admin {
			return fmt.Errorf("%q: adminUnlessGroup only makes sense with admin: true", f.Path)
		}
		if !groupRe.MatchString(f.AdminUnlessGroup) {
			return fmt.Errorf("%q: adminUnlessGroup %q is not a group name", f.Path, f.AdminUnlessGroup)
		}
	}
	// ~ is the signed-in user's home; on the root bridge it would be root's.
	if f.Admin && strings.HasPrefix(f.Path, "~") {
		return fmt.Errorf("%q: an admin folder must be an absolute path, not under ~", f.Path)
	}
	return nil
}

func (h *HTTPAPI) validate(declared []string) error {
	on := checkOn(declared)
	if !httpNameRe.MatchString(h.Name) {
		return fmt.Errorf("name must be lowercase letters, digits or - (max 32), starting with a letter")
	}
	// 107 bytes: the size of sun_path without its NUL. A Windows-only entry
	// may name a pipe instead.
	if isPipe(h.Socket) {
		if !pipeOnly(declared) || !pipeRe.MatchString(h.Socket) {
			return fmt.Errorf(`socket %q: a named pipe is \\.\pipe\<name>, in an entry for windows alone`, h.Socket)
		}
	} else if !validAbsOn(h.Socket, false, on) || h.Socket == "/" || len(h.Socket) > 107 {
		return fmt.Errorf("socket %q must be a clean absolute path of at most 107 bytes", h.Socket)
	}
	if h.AdminUnlessGroup != "" {
		if !h.Admin {
			return fmt.Errorf("adminUnlessGroup only makes sense with admin: true")
		}
		if !groupRe.MatchString(h.AdminUnlessGroup) {
			return fmt.Errorf("adminUnlessGroup %q is not a group name", h.AdminUnlessGroup)
		}
	}
	if len(h.Headers) > maxHTTPHeaders {
		return fmt.Errorf("at most %d headers", maxHTTPHeaders)
	}
	seen := map[string]bool{}
	for _, name := range h.Headers {
		if !headerNameRe.MatchString(name) {
			return fmt.Errorf("%q is not a header name", name)
		}
		if !safeHeader(name) {
			return fmt.Errorf("header %q may not be set by a plugin", name)
		}
		k := http.CanonicalHeaderKey(name)
		if seen[k] {
			return fmt.Errorf("header %q is listed twice", name)
		}
		seen[k] = true
	}
	if err := h.validateRemote(); err != nil {
		return err
	}
	if len(h.Rules) == 0 || len(h.Rules) > maxHTTPRules {
		return fmt.Errorf("rules must hold between 1 and %d entries", maxHTTPRules)
	}
	for i := range h.Rules {
		r := &h.Rules[i]
		if len(r.Methods) == 0 || len(r.Methods) > len(httpMethods) {
			return fmt.Errorf("rules[%d] needs methods", i)
		}
		for _, m := range r.Methods {
			if !httpMethods[m] {
				return fmt.Errorf("rules[%d]: method %q must be one of GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS", i, m)
			}
		}
		if r.Path == "" || len(r.Path) > 512 {
			return fmt.Errorf("rules[%d] needs a path (a regular expression the whole URL path must match)", i)
		}
		re, err := compileSpec(r.Path)
		if err != nil {
			return fmt.Errorf("rules[%d] path: %v", i, err)
		}
		r.re = re
	}
	if h.MaxBody < 0 || h.MaxBody > maxHTTPBodyLimit {
		return fmt.Errorf("maxBody must be between 0 and %d", maxHTTPBodyLimit)
	}
	if h.MaxUpload < 0 || h.MaxUpload > maxHTTPUploadLimit {
		return fmt.Errorf("maxUpload must be between 0 and %d", maxHTTPUploadLimit)
	}
	if h.TimeoutSec < 0 || h.TimeoutSec > 600 {
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

// RunsRoot reports whether any command, HTTP API or folder may be used
// with administrator rights.
func (m *Manifest) RunsRoot() bool {
	c := &m.Capabilities
	for _, x := range c.Commands {
		if x.Admin {
			return true
		}
	}
	for _, x := range c.HTTP {
		if x.Admin {
			return true
		}
	}
	for _, l := range [][]Folder{c.Files.Read, c.Files.Write} {
		for _, f := range l {
			if f.Admin {
				return true
			}
		}
	}
	return false
}

func compileSpec(p string) (*regexp.Regexp, error) {
	return regexp.Compile(`^(?:` + p + `)$`)
}
