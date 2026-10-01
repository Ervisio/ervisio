package software

import (
	"bufio"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// App is an installed desktop application (from a .desktop file).
type App struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Comment    string   `json:"comment"`
	Icon       string   `json:"icon"`
	Exec       string   `json:"exec"`
	Categories []string `json:"categories"`
	Package    string   `json:"package"`
	Source     string   `json:"source"`
	Kind       string   `json:"kind"`
	Version    string   `json:"version"`
	Scope      string   `json:"scope,omitempty"`
	// Explicit is true when the user installed the package (not a dependency).
	Explicit bool `json:"explicit"`

	file string
}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// desktopDirs lists where applications are described, with flatpak exports.
func desktopDirs() []string {
	dirs := []string{"/usr/share/applications", "/usr/local/share/applications", "/var/lib/flatpak/exports/share/applications"}
	if h := home(); h != "" {
		dirs = append(dirs, filepath.Join(h, ".local/share/applications"), filepath.Join(h, ".local/share/flatpak/exports/share/applications"))
	}
	return dirs
}

var localeKeyRe = regexp.MustCompile(`^(Name|Comment)\[`)

// parseDesktop reads the [Desktop Entry] group. It returns ok=false for
// entries that must not be listed (hidden, not applications).
func parseDesktop(content string) (a App, ok bool) {
	in := false
	var typ string
	hidden := false
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			in = line == "[Desktop Entry]"
			continue
		}
		if !in || line == "" || line[0] == '#' || localeKeyRe.MatchString(line) {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Type":
			typ = v
		case "Name":
			a.Name = v
		case "Comment":
			a.Comment = v
		case "Icon":
			a.Icon = v
		case "Exec":
			a.Exec = v
		case "Categories":
			for _, c := range strings.Split(v, ";") {
				if c != "" {
					a.Categories = append(a.Categories, c)
				}
			}
		case "NoDisplay", "Hidden":
			if v == "true" {
				hidden = true
			}
		}
	}
	if typ != "Application" || hidden || a.Name == "" {
		return a, false
	}
	if a.Categories == nil {
		a.Categories = []string{}
	}
	return a, true
}

// scanApps reads every .desktop file, once per id (user files win).
func scanApps() []App {
	byID := map[string]App{}
	dirs := desktopDirs()
	for i := len(dirs) - 1; i >= 0; i-- { // later dirs (user) override
		ents, err := os.ReadDir(dirs[i])
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			p := filepath.Join(dirs[i], e.Name())
			b, err := os.ReadFile(p)
			if err != nil || len(b) > 256<<10 {
				continue
			}
			a, ok := parseDesktop(string(b))
			if !ok {
				delete(byID, e.Name())
				continue
			}
			a.ID, a.file = strings.TrimSuffix(e.Name(), ".desktop"), p
			if strings.Contains(p, "flatpak/exports") {
				a.Kind, a.Source, a.Package = KindFlatpak, "flatpak", a.ID
				a.Scope = "system"
				if strings.Contains(p, "/.local/") {
					a.Scope = "user"
				}
			}
			byID[e.Name()] = a
		}
	}
	apps := make([]App, 0, len(byID))
	for _, a := range byID {
		apps = append(apps, a)
	}
	sort.Slice(apps, func(i, j int) bool { return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name) })
	return apps
}

// ---- icons ----

// Icon is the answer of software.icon.
type Icon struct {
	Mime string `json:"mime"`
	Data string `json:"data"` // base64
}

func iconBases() []string {
	b := []string{"/usr/share/icons", "/usr/local/share/icons", "/var/lib/flatpak/exports/share/icons"}
	if h := home(); h != "" {
		b = append(b, filepath.Join(h, ".local/share/icons"), filepath.Join(h, ".icons"), filepath.Join(h, ".local/share/flatpak/exports/share/icons"))
	}
	return b
}

var iconThemes = []string{"hicolor", "Adwaita", "breeze", "breeze-dark", "AdwaitaLegacy", "gnome", "Papirus", "Yaru"}
var iconSizes = []int{16, 22, 24, 32, 48, 64, 96, 128, 256, 512}

type iconKey struct {
	name string
	size int
}

var (
	iconMu    sync.Mutex
	iconCache = map[iconKey]string{} // resolved path ("" = none)
)

// iconDirs returns candidate directories (relative to a theme) for apps icons,
// ordered by how well they match size.
func iconDirs(size int) []string {
	sorted := append([]int(nil), iconSizes...)
	sort.Slice(sorted, func(i, j int) bool {
		di, dj := sorted[i]-size, sorted[j]-size
		// prefer the nearest size not smaller, then the nearest smaller
		if (di >= 0) != (dj >= 0) {
			return di >= 0
		}
		if di < 0 {
			di, dj = -di, -dj
		}
		return di < dj
	})
	var dirs []string
	for _, s := range sorted {
		n := strconv.Itoa(s)
		dirs = append(dirs, n+"x"+n+"/apps", "apps/"+n)
	}
	return append(dirs, "scalable/apps", "apps/scalable")
}

// findIcon locates an icon file by theme name.
func findIcon(name string, size int) string {
	k := iconKey{name, size}
	iconMu.Lock()
	if p, ok := iconCache[k]; ok {
		iconMu.Unlock()
		return p
	}
	iconMu.Unlock()
	p := searchIcon(name, size)
	iconMu.Lock()
	iconCache[k] = p
	iconMu.Unlock()
	return p
}

func searchIcon(name string, size int) string {
	if p := searchIconIn(iconBases(), name, size); p != "" {
		return p
	}
	for _, e := range []string{".png", ".svg"} {
		p := filepath.Join("/usr/share/pixmaps", name+e)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

func searchIconIn(bases []string, name string, size int) string {
	dirs := iconDirs(size)
	exts := []string{".png", ".svg"}
	for _, th := range iconThemes {
		for _, base := range bases {
			for _, d := range dirs {
				for _, e := range exts {
					p := filepath.Join(base, th, d, name+e)
					if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
						return p
					}
				}
			}
		}
	}
	return ""
}

var iconNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@-]*$`)

// allowedIconRoots limits absolute icon paths.
func allowedIconRoots() []string {
	r := []string{"/usr/share/", "/usr/local/share/", "/opt/", "/var/lib/flatpak/", "/usr/lib/"}
	if h := home(); h != "" {
		r = append(r, h+"/.local/share/", h+"/.icons/")
	}
	return r
}

func iconMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	}
	return ""
}

// loadIcon resolves a .desktop Icon value (theme name or absolute path).
func loadIcon(name string, size int) (*Icon, error) {
	if size <= 0 || size > 1024 {
		size = 64
	}
	var path string
	if strings.HasPrefix(name, "/") {
		clean := filepath.Clean(name)
		ok := false
		for _, r := range allowedIconRoots() {
			if strings.HasPrefix(clean, r) {
				ok = true
			}
		}
		if !ok || iconMime(clean) == "" {
			return nil, rpc.Errorf(rpc.NotFound, "Icon not found.")
		}
		path = clean
	} else {
		if !iconNameRe.MatchString(name) {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid icon name.")
		}
		path = findIcon(name, size)
	}
	if path == "" {
		return nil, rpc.Errorf(rpc.NotFound, "Icon %q is not in any icon theme.", name)
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 1536<<10 {
		return nil, rpc.Errorf(rpc.NotFound, "Icon not found.")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, rpc.Errorf(rpc.NotFound, "Icon not found.")
	}
	return &Icon{Mime: iconMime(path), Data: base64.StdEncoding.EncodeToString(b)}, nil
}

// resolveApps adds package and version information to apps.
func (m *manager) resolveApps(ctx context.Context, apps []App) {
	var files []string
	idx := map[string]int{}
	for i := range apps {
		if apps[i].Kind != KindFlatpak {
			files = append(files, apps[i].file)
			idx[apps[i].file] = i
		}
	}
	if m.primary != nil && len(files) > 0 {
		if of, ok := m.primary.(ownerFinder); ok {
			for f, pkg := range of.Owners(ctx, files) {
				if i, ok := idx[f]; ok {
					apps[i].Package = pkg
				}
			}
		}
	}
	inst, _ := m.installed(ctx, false)
	byName := map[string]Package{}
	for _, p := range inst {
		byName[p.Name+"|"+p.Kind] = p
	}
	for i := range apps {
		a := &apps[i]
		if a.Kind == KindFlatpak {
			if p, ok := byName[a.Package+"|"+KindFlatpak]; ok {
				a.Version, a.Explicit = p.Version, true
			}
			continue
		}
		if a.Package == "" {
			continue
		}
		if p, ok := byName[a.Package+"|"+KindRepo]; ok {
			a.Version, a.Source, a.Kind, a.Explicit = p.Version, p.Source, p.Kind, p.Reason == "explicit"
		} else if p, ok := byName[a.Package+"|"+KindAUR]; ok {
			a.Version, a.Source, a.Kind, a.Explicit = p.Version, p.Source, p.Kind, p.Reason == "explicit"
		}
	}
}
