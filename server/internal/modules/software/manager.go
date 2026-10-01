package software

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

const cacheTTL = 10 * time.Minute

// manager owns the detected backends and the caches.
type manager struct {
	primary Backend // pacman, apt, dnf or zypper (nil when none is installed)
	aur     Backend // yay or paru (nil when absent)
	flatpak Backend // nil when absent
	pac     *pacman
	cache   ttlCache
}

var (
	mgrOnce sync.Once
	mgr     *manager
)

func getManager() *manager {
	mgrOnce.Do(func() { mgr = newManager() })
	return mgr
}

func newManager() *manager {
	m := &manager{}
	if p := newPacman(); p.Available() {
		m.primary, m.pac = p, p
		if a := newAUR(); a.Available() {
			m.aur = a
		}
	} else if a := newApt(); a.Available() {
		m.primary = a
	} else if d := newDnf(); d.Available() {
		m.primary = d
	} else if z := newZypper(); z.Available() {
		m.primary = z
	}
	if f := newFlatpak(); f.Available() {
		m.flatpak = f
	}
	return m
}

func (m *manager) backends() []Backend {
	var out []Backend
	for _, b := range []Backend{m.primary, m.aur, m.flatpak} {
		if b != nil {
			out = append(out, b)
		}
	}
	return out
}

func (m *manager) requireAny() error {
	if len(m.backends()) == 0 {
		return rpc.Errorf(rpc.Unavailable, "No supported package manager was found on this system (pacman, apt, dnf, zypper or flatpak).")
	}
	return nil
}

// sourceNames lists the kinds of source available.
func (m *manager) sourceNames() []string {
	out := []string{}
	for _, b := range m.backends() {
		out = append(out, b.Kind())
	}
	return out
}

// invalidate drops every cache after the system changed.
func (m *manager) invalidate() {
	m.cache.clear()
	if m.pac != nil {
		m.pac.forget()
	}
}

var kindOrder = map[string]int{KindRepo: 0, KindAUR: 1, KindFlatpak: 2}

// updates returns every pending upgrade, cached for ten minutes.
func (m *manager) updates(ctx context.Context, force bool) ([]Update, time.Time, []string, error) {
	type res struct {
		ups   []Update
		warns []string
	}
	v, at, err := m.cache.get("updates", cacheTTL, force, func() (any, error) {
		bs := m.backends()
		results := make([][]Update, len(bs))
		errs := make([]error, len(bs))
		var wg sync.WaitGroup
		for i, b := range bs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i], errs[i] = b.ListUpdates(ctx)
			}()
		}
		wg.Wait()
		out := res{ups: []Update{}}
		failed := 0
		for i, b := range bs {
			if errs[i] != nil {
				failed++
				out.warns = append(out.warns, b.Name()+": "+errs[i].Error())
				continue
			}
			out.ups = append(out.ups, results[i]...)
		}
		if failed == len(bs) && failed > 0 {
			return nil, errs[0]
		}
		sort.SliceStable(out.ups, func(i, j int) bool {
			a, b := out.ups[i], out.ups[j]
			if kindOrder[a.Kind] != kindOrder[b.Kind] {
				return kindOrder[a.Kind] < kindOrder[b.Kind]
			}
			return a.Name < b.Name
		})
		return out, nil
	})
	if err != nil {
		return nil, time.Time{}, nil, err
	}
	r := v.(res)
	return r.ups, at, r.warns, nil
}

// installed returns every installed package and Flatpak app.
func (m *manager) installed(ctx context.Context, force bool) ([]Package, error) {
	v, _, err := m.cache.get("installed", cacheTTL, force, func() (any, error) {
		all := []Package{}
		var firstErr error
		for _, b := range m.backends() {
			p, err := b.ListInstalled(ctx)
			if err != nil {
				if firstErr == nil && b == m.primary {
					firstErr = err
				}
				continue
			}
			all = append(all, p...)
		}
		if len(all) == 0 && firstErr != nil {
			return nil, firstErr
		}
		return all, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Package), nil
}

func filterInstalled(pkgs []Package, filter string) ([]Package, error) {
	out := make([]Package, 0, len(pkgs))
	for _, p := range pkgs {
		keep := false
		switch filter {
		case "", "all":
			keep = true
		case "explicit":
			keep = p.Reason == "explicit" && p.Kind != KindFlatpak
		case "deps":
			keep = p.Reason == "dependency"
		case "orphans":
			keep = p.Orphan
		case "aur":
			keep = p.Kind == KindAUR
		case "flatpak":
			keep = p.Kind == KindFlatpak
		default:
			return nil, rpc.Errorf(rpc.Invalid, "Unknown filter %q (use all, explicit, deps, orphans, aur or flatpak).", filter)
		}
		if keep {
			out = append(out, p)
		}
	}
	return out, nil
}

// backendFor picks the backend that serves a source (a repo name, "aur", "flatpak" or a kind).
func (m *manager) backendFor(source string) (Backend, error) {
	switch source {
	case KindAUR:
		if m.aur == nil {
			return nil, rpc.Errorf(rpc.Unavailable, "No AUR helper (yay or paru) is installed.")
		}
		return m.aur, nil
	case "flatpak":
		if m.flatpak == nil {
			return nil, rpc.Errorf(rpc.Unavailable, "Flatpak is not installed.")
		}
		return m.flatpak, nil
	}
	if m.primary == nil {
		return nil, rpc.Errorf(rpc.Unavailable, "No supported system package manager was found.")
	}
	return m.primary, nil
}

// search queries every backend in parallel.
func (m *manager) search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []Result{}, nil
	}
	if len(q) > 100 {
		return nil, rpc.Errorf(rpc.Invalid, "The search text is too long.")
	}
	bs := m.backends()
	results := make([][]Result, len(bs))
	errs := make([]error, len(bs))
	var wg sync.WaitGroup
	for i, b := range bs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = b.Search(ctx, q)
		}()
	}
	wg.Wait()
	all := []Result{}
	failed := 0
	for i := range bs {
		if errs[i] != nil {
			failed++
			continue
		}
		r := results[i]
		if limit := map[string]int{KindRepo: 40, KindAUR: 20, KindFlatpak: 24}[bs[i].Kind()]; len(r) > limit {
			r = r[:limit]
		}
		all = append(all, r...)
	}
	if failed == len(bs) && failed > 0 {
		return nil, errs[0]
	}
	low := strings.ToLower(q)
	noise := regexp.MustCompile(`-(i18n|l10n|help|lang|langpack|locale|docs?|debug|dbg)(-|$)`)
	rank := func(r Result) int {
		n := strings.ToLower(r.Name)
		t := strings.ToLower(r.Title)
		if r.Kind == KindFlatpak { // org.gimp.GIMP matches "gimp"
			n = n[strings.LastIndex(n, ".")+1:]
		}
		if noise.MatchString(n) {
			return 4
		}
		switch {
		case n == low || t == low:
			return 0
		case strings.HasPrefix(n, low) || strings.HasPrefix(t, low):
			return 1
		case strings.Contains(n, low) || strings.Contains(t, low):
			return 2
		}
		return 3
	}
	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := rank(all[i]), rank(all[j])
		if ri != rj {
			return ri < rj
		}
		return kindOrder[all[i].Kind] < kindOrder[all[j].Kind]
	})
	return all, nil
}

// catalog is a short curated list of well-known software for the store's
// categories; names that a distribution does not have are simply skipped.
var catalog = map[string]struct{ pkgs, flatpaks []string }{
	"popular": {
		[]string{"firefox", "chromium", "thunderbird", "gimp", "vlc", "obs-studio", "libreoffice-fresh", "libreoffice", "inkscape", "blender", "krita", "audacity"},
		[]string{"org.mozilla.firefox", "org.mozilla.Thunderbird", "org.gimp.GIMP", "org.videolan.VLC", "com.obsproject.Studio", "org.blender.Blender", "com.spotify.Client", "com.discordapp.Discord"},
	},
	"development": {
		[]string{"git", "code", "neovim", "vim", "docker", "gdb", "python", "nodejs", "go", "rustup", "podman", "cmake"},
		[]string{"com.visualstudio.code", "io.dbeaver.DBeaverCommunity", "com.jetbrains.IntelliJ-IDEA-Community"},
	},
	"internet": {
		[]string{"firefox", "chromium", "thunderbird", "qbittorrent", "filezilla", "transmission-gtk", "wget", "curl"},
		[]string{"org.mozilla.firefox", "org.mozilla.Thunderbird", "com.discordapp.Discord", "org.telegram.desktop", "org.signal.Signal"},
	},
	"graphics": {
		[]string{"gimp", "inkscape", "krita", "blender", "darktable", "scribus"},
		[]string{"org.gimp.GIMP", "org.inkscape.Inkscape", "org.blender.Blender", "org.kde.krita"},
	},
	"media": {
		[]string{"vlc", "mpv", "obs-studio", "audacity", "handbrake", "kdenlive", "ffmpeg"},
		[]string{"org.videolan.VLC", "com.obsproject.Studio", "com.spotify.Client", "org.kde.kdenlive"},
	},
	"system": {
		[]string{"htop", "btop", "gparted", "timeshift", "ncdu", "tmux", "neofetch", "rsync"},
		[]string{"com.github.tchx84.Flatseal", "org.gnome.baobab", "io.missioncenter.MissionCenter"},
	},
	"servers": {
		[]string{"nginx", "apache", "apache2", "httpd", "postgresql", "mariadb", "mysql-server", "redis", "openssh", "fail2ban", "samba", "docker"},
		nil,
	},
}

func (m *manager) suggest(ctx context.Context, category string) ([]Result, error) {
	if category == "" {
		category = "popular"
	}
	c, ok := catalog[category]
	if !ok {
		return nil, rpc.Errorf(rpc.Invalid, "Unknown category %q.", category)
	}
	v, _, err := m.cache.get("suggest:"+category, cacheTTL, false, func() (any, error) {
		out := []Result{}
		var wg sync.WaitGroup
		var mu sync.Mutex
		if lk, ok := m.primary.(lookuper); ok && m.primary != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, _ := lk.Lookup(ctx, c.pkgs)
				mu.Lock()
				out = append(out, r...)
				mu.Unlock()
			}()
		}
		if lk, ok := m.flatpak.(lookuper); ok && m.flatpak != nil && len(c.flatpaks) > 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, _ := lk.Lookup(ctx, c.flatpaks)
				mu.Lock()
				out = append(out, r...)
				mu.Unlock()
			}()
		}
		wg.Wait()
		sort.SliceStable(out, func(i, j int) bool { return kindOrder[out[i].Kind] < kindOrder[out[j].Kind] })
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Result), nil
}

// rebootPending reports whether the running system lags behind what is installed.
func rebootPending() bool {
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		return true
	}
	rel, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	if _, err := os.Stat("/usr/lib/modules"); err != nil {
		return false
	}
	if _, err := os.Stat("/usr/lib/modules/" + strings.TrimSpace(string(rel))); err != nil {
		return true
	}
	return false
}
