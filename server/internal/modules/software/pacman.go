package software

import (
	"context"
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// pacman is the Arch Linux package manager. Reading the installed list goes
// straight to the local database; updates use a private copy of the sync
// databases (like checkupdates) so no root is needed and the system databases
// are never partially refreshed.
type pacman struct {
	dbOnce sync.Once
	dbPath string
	tmpMu  sync.Mutex
}

func newPacman() *pacman { return &pacman{} }

func (p *pacman) Name() string    { return "pacman" }
func (p *pacman) Kind() string    { return KindRepo }
func (p *pacman) Available() bool { return have("pacman") }

func (p *pacman) db() string {
	p.dbOnce.Do(func() {
		p.dbPath = "/var/lib/pacman"
		if out, err := run(context.Background(), 10*time.Second, nil, "pacman-conf", "DBPath"); err == nil {
			if s := strings.TrimSpace(out); s != "" {
				p.dbPath = strings.TrimRight(s, "/")
			}
		}
	})
	return p.dbPath
}

// ---- installed ----

// parseLocalDesc reads one /var/lib/pacman/local/<pkg>/desc file.
func parseLocalDesc(r string) Package {
	p := Package{Reason: "explicit", Kind: KindRepo}
	sections := map[string]string{}
	var cur string
	for _, line := range strings.Split(r, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") {
			cur = line
			continue
		}
		if line != "" && cur != "" {
			if _, ok := sections[cur]; !ok {
				sections[cur] = line
			}
		}
	}
	p.Name = sections["%NAME%"]
	p.Version = sections["%VERSION%"]
	p.Description = sections["%DESC%"]
	p.Size, _ = strconv.ParseInt(sections["%SIZE%"], 10, 64)
	p.InstallDate, _ = strconv.ParseInt(sections["%INSTALLDATE%"], 10, 64)
	if sections["%REASON%"] == "1" {
		p.Reason = "dependency"
	}
	return p
}

// parseSyncList parses `pacman -Sl` into package name -> repository.
func parseSyncList(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if _, ok := m[f[1]]; !ok {
			m[f[1]] = f[0]
		}
	}
	return m
}

func (p *pacman) ListInstalled(ctx context.Context) ([]Package, error) {
	dir := filepath.Join(p.db(), "local")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, rpc.Errorf(rpc.Internal, "cannot read the pacman database: %v", err)
	}
	repos := map[string]string{}
	if out, err := run(ctx, 30*time.Second, nil, "pacman", "-Sl"); err == nil {
		repos = parseSyncList(out)
	}
	orphans := map[string]bool{}
	if out, _ := run(ctx, 30*time.Second, nil, "pacman", "-Qdtq"); out != "" { // exit 1 when there are none
		for _, n := range strings.Fields(out) {
			orphans[n] = true
		}
	}
	pkgs := make([]Package, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "desc"))
		if err != nil {
			continue
		}
		pk := parseLocalDesc(string(b))
		if pk.Name == "" {
			continue
		}
		if r, ok := repos[pk.Name]; ok {
			pk.Source = r
		} else {
			pk.Source, pk.Kind = "aur", KindAUR
		}
		pk.Orphan = orphans[pk.Name]
		pkgs = append(pkgs, pk)
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs, nil
}

// ---- updates ----

// checkDBBase returns the folder that holds the private sync copy: root's
// /var/cache/ervisio, or the user's own cache folder. Never a shared
// directory such as /tmp, where another user could plant it first.
var checkDBBase = func() (string, error) {
	if isRoot() {
		return brand.CacheDir, nil
	}
	if d := os.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, brand.Slug), nil
	}
	home := ""
	if u, err := user.Current(); err == nil {
		home = u.HomeDir
	}
	if home == "" {
		home = os.Getenv("HOME")
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("no home folder for the private package database")
	}
	return filepath.Join(home, ".cache", brand.Slug), nil
}

// checkDir returns the private sync copy's folder (it may not exist yet).
func (p *pacman) checkDir() (string, error) {
	base, err := checkDBBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "checkdb"), nil
}

// dbArgs selects the private sync copy when it is newer than the system one,
// and only when it is still a 0700 folder of ours.
func (p *pacman) dbArgs() []string {
	tmp, err := p.checkDir()
	if err != nil || ensurePrivateDir(tmp, false) != nil {
		return nil
	}
	ti, err := os.Lstat(filepath.Join(tmp, "sync", "core.db"))
	if err != nil || !ti.Mode().IsRegular() {
		return nil
	}
	si, err := os.Stat(filepath.Join(p.db(), "sync", "core.db"))
	if err == nil && si.ModTime().After(ti.ModTime()) {
		return nil
	}
	return []string{"--dbpath", tmp}
}

// forget drops the private sync copy (after a real transaction).
func (p *pacman) forget() {
	if tmp, err := p.checkDir(); err == nil && ensurePrivateDir(tmp, false) == nil {
		_ = os.RemoveAll(tmp)
	}
}

// prepareCheckDB creates the private copy: a 0700 folder of ours holding a
// link to the system's local database and copies of its sync databases.
// Anything in it that is not what we put there is replaced.
func (p *pacman) prepareCheckDB() (string, error) {
	tmp, err := p.checkDir()
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDir(tmp, true); err != nil {
		return "", rpc.Errorf(rpc.Internal, "The private package database folder is not safe to use: %v", err)
	}
	local := filepath.Join(tmp, "local")
	want := filepath.Join(p.db(), "local")
	if t, err := os.Readlink(local); err != nil || t != want {
		if err := os.RemoveAll(local); err != nil {
			return "", err
		}
		if err := os.Symlink(want, local); err != nil {
			return "", err
		}
	}
	syncDir := filepath.Join(tmp, "sync")
	if fi, err := os.Lstat(syncDir); err == nil && (!fi.IsDir() || ownerOf(fi) != os.Geteuid()) {
		if err := os.RemoveAll(syncDir); err != nil {
			return "", err
		}
	}
	if err := os.Mkdir(syncDir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	// Start from the system sync databases, so only changes are downloaded.
	if ents, err := os.ReadDir(filepath.Join(p.db(), "sync")); err == nil {
		for _, e := range ents {
			dst := filepath.Join(syncDir, e.Name())
			if fi, err := os.Lstat(dst); err == nil {
				if fi.Mode().IsRegular() && ownerOf(fi) == os.Geteuid() {
					continue
				}
				if err := os.RemoveAll(dst); err != nil {
					return "", err
				}
			}
			copyFile(filepath.Join(p.db(), "sync", e.Name()), dst)
		}
	}
	return tmp, nil
}

func (p *pacman) Refresh(ctx context.Context) error {
	p.tmpMu.Lock()
	defer p.tmpMu.Unlock()
	tmp, err := p.prepareCheckDB()
	if err != nil {
		return err
	}
	args := []string{"-Sy", "--disable-sandbox", "--dbpath", tmp, "--logfile", "/dev/null"}
	name, full := "pacman", args
	if !isRoot() {
		if !have("fakeroot") {
			return rpc.Errorf(rpc.Unavailable, "Checking for updates without administrator rights needs fakeroot (package fakeroot). Showing the last known state.")
		}
		name, full = "fakeroot", append([]string{"--", "pacman"}, args...)
	}
	_, err = run(ctx, 2*time.Minute, nil, name, full...)
	if err != nil {
		return rpc.Errorf(rpc.Unavailable, "Could not refresh the package databases: %v", err)
	}
	return nil
}

// copyFile copies a system sync database into the private folder. The target
// is created with O_EXCL|O_NOFOLLOW, so it is never written through a link.
func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY|oNoFollow, 0o600)
	if err != nil {
		return
	}
	_, cerr := io.Copy(out, in)
	if err := out.Close(); cerr == nil {
		cerr = err
	}
	if cerr != nil {
		os.Remove(dst)
		return
	}
	if fi, err := in.Stat(); err == nil {
		_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
}

// parseQu parses `pacman -Qu` ("name old -> new").
func parseQu(out string) []Update {
	var ups []Update
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[2] != "->" {
			continue
		}
		if strings.Contains(line, "[ignored]") {
			continue
		}
		ups = append(ups, Update{Name: f[0], From: f[1], To: f[3], Kind: KindRepo, Notes: []string{}})
	}
	return ups
}

type sizeInfo struct {
	to, repo string
	size     int64
}

// parseSup parses `pacman -Sup --print-format '%n|%v|%s|%r'`.
func parseSup(out string) map[string]sizeInfo {
	m := map[string]sizeInfo{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 4 {
			continue
		}
		sz, _ := strconv.ParseInt(f[2], 10, 64)
		m[f[0]] = sizeInfo{to: f[1], size: sz, repo: f[3]}
	}
	return m
}

func (p *pacman) ListUpdates(ctx context.Context) ([]Update, error) {
	db := p.dbArgs()
	out, err := run(ctx, time.Minute, nil, "pacman", append([]string{"-Qu"}, db...)...)
	if err != nil && exitCode(err) != 1 { // 1 = nothing to update
		return nil, err
	}
	ups := parseQu(out)
	sizes := map[string]sizeInfo{}
	if sup, err := run(ctx, time.Minute, nil, "pacman", append([]string{"-Sup", "--print-format", "%n|%v|%s|%r"}, db...)...); sup != "" || err == nil {
		sizes = parseSup(sup)
	}
	seen := map[string]bool{}
	for i := range ups {
		seen[ups[i].Name] = true
		if si, ok := sizes[ups[i].Name]; ok {
			ups[i].Size, ups[i].Source = si.size, si.repo
		}
		if ups[i].Source == "" {
			ups[i].Source = "repo"
		}
	}
	// New dependencies the upgrade pulls in.
	for n, si := range sizes {
		if !seen[n] {
			ups = append(ups, Update{Name: n, To: si.to, Source: si.repo, Kind: KindRepo, Size: si.size, Notes: []string{}})
		}
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].Name < ups[j].Name })
	p.annotate(ctx, ups)
	return ups, nil
}

var serviceFileRe = regexp.MustCompile(`^(\S+) /usr/lib/systemd/system/([^/]+\.service)$`)

// parseServiceFiles extracts package -> units from `pacman -Ql`.
func parseServiceFiles(out string) map[string][]string {
	m := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		if g := serviceFileRe.FindStringSubmatch(strings.TrimSpace(line)); g != nil {
			m[g[1]] = append(m[g[1]], g[2])
		}
	}
	return m
}

// annotate adds reboot, security and restartService notes.
func (p *pacman) annotate(ctx context.Context, ups []Update) {
	var names []string
	for i := range ups {
		if needsReboot(ups[i].Name) {
			ups[i].Notes = append(ups[i].Notes, "reboot")
		}
		names = append(names, ups[i].Name)
	}
	if len(names) > 0 && len(names) <= 300 {
		var installed []string
		for i := range ups {
			if ups[i].From != "" {
				installed = append(installed, ups[i].Name)
			}
		}
		// Stream the (huge) file list and keep only systemd service files.
		var keep strings.Builder
		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_ = sys.Stream(sctx, "pacman", append([]string{"-Ql", "--"}, installed...), func(line string) error {
			if strings.Contains(line, " /usr/lib/systemd/system/") && strings.HasSuffix(line, ".service") {
				keep.WriteString(line + "\n")
			}
			return nil
		})
		cancel()
		if out := keep.String(); out != "" {
			units := parseServiceFiles(out)
			var all []string
			for _, us := range units {
				all = append(all, us...)
			}
			if len(all) > 0 {
				active := activeUnits(ctx, all)
				for i := range ups {
					for _, u := range units[ups[i].Name] {
						if active[u] {
							ups[i].Notes = append(ups[i].Notes, "restartService:"+u)
							break
						}
					}
				}
			}
		}
	}
	if have("arch-audit") {
		if out, _ := run(ctx, time.Minute, nil, "arch-audit", "--upgradable", "--format", "%n"); out != "" {
			vuln := map[string]bool{}
			for _, n := range strings.Fields(out) {
				vuln[n] = true
			}
			for i := range ups {
				if vuln[ups[i].Name] {
					ups[i].Notes = append(ups[i].Notes, "security")
				}
			}
		}
	}
}

// activeUnits returns which of the units are currently active.
func activeUnits(ctx context.Context, units []string) map[string]bool {
	res := map[string]bool{}
	if !have("systemctl") || len(units) == 0 {
		return res
	}
	out, _ := run(ctx, 15*time.Second, nil, "systemctl", append([]string{"is-active", "--"}, units...)...)
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if i < len(units) && strings.TrimSpace(line) == "active" {
			res[units[i]] = true
		}
	}
	return res
}

// ---- search / info ----

// parseSearch parses `pacman -Ss` / `yay -Ss` output.
func parseSearch(out string, kind string) []Result {
	var res []Result
	lines := strings.Split(out, "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l == "" || l[0] == ' ' || l[0] == '\t' {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		repo, name, ok := strings.Cut(f[0], "/")
		if !ok {
			continue
		}
		r := Result{Name: name, Version: f[1], Source: repo, Kind: kind}
		low := strings.ToLower(l)
		r.Installed = strings.Contains(low, "[installed")
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], " ") {
			r.Description = strings.TrimSpace(lines[i+1])
			i++
		}
		res = append(res, r)
	}
	return res
}

func searchTerms(q string) []string {
	var out []string
	for _, t := range strings.Fields(q) {
		out = append(out, regexp.QuoteMeta(t))
	}
	return out
}

func (p *pacman) Search(ctx context.Context, q string) ([]Result, error) {
	terms := searchTerms(q)
	if len(terms) == 0 {
		return nil, nil
	}
	out, err := run(ctx, 30*time.Second, nil, "pacman", append([]string{"-Ss", "--"}, terms...)...)
	if err != nil && exitCode(err) != 1 {
		return nil, err
	}
	return parseSearch(out, KindRepo), nil
}

func (p *pacman) Lookup(ctx context.Context, names []string) ([]Result, error) {
	out, _ := run(ctx, 30*time.Second, nil, "pacman", append([]string{"-Si", "--"}, names...)...)
	var res []Result
	for _, block := range strings.Split(out, "\n\n") {
		f := parseKV(block)
		n := fieldValue(f, "Name")
		if n == "" {
			continue
		}
		res = append(res, Result{Name: n, Version: fieldValue(f, "Version"), Source: fieldValue(f, "Repository"), Kind: KindRepo, Description: fieldValue(f, "Description")})
	}
	if len(res) > 0 {
		if inst, err := run(ctx, 15*time.Second, nil, "pacman", "-Qq"); err == nil {
			set := map[string]bool{}
			for _, n := range strings.Fields(inst) {
				set[n] = true
			}
			for i := range res {
				res[i].Installed = set[res[i].Name]
			}
		}
	}
	return res, nil
}

func (p *pacman) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	installed := true
	out, err := run(ctx, 20*time.Second, nil, "pacman", "-Qi", "--", name)
	if err != nil {
		installed = false
		out, err = run(ctx, 20*time.Second, nil, "pacman", "-Si", "--", name)
		if err != nil {
			return nil, rpc.Errorf(rpc.NotFound, "No package called %q was found.", name)
		}
	}
	f := parseKV(out)
	return &Detail{Name: name, Source: fieldValue(f, "Repository"), Kind: KindRepo, Installed: installed,
		Version: fieldValue(f, "Version"), Description: fieldValue(f, "Description"), Fields: f}, nil
}

func (p *pacman) Owners(ctx context.Context, files []string) map[string]string {
	res := map[string]string{}
	for len(files) > 0 {
		n := len(files)
		if n > 200 {
			n = 200
		}
		out, _ := run(ctx, 30*time.Second, nil, "pacman", append([]string{"-Qo", "--"}, files[:n]...)...)
		for k, v := range parseOwned(out) {
			res[k] = v
		}
		files = files[n:]
	}
	return res
}

var ownedRe = regexp.MustCompile(`^(.+) is owned by (\S+) \S+$`)

func parseOwned(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if g := ownedRe.FindStringSubmatch(strings.TrimSpace(line)); g != nil {
			m[g[1]] = g[2]
		}
	}
	return m
}

// ---- plans ----

var (
	pacmanStepRe  = regexp.MustCompile(`^\((\d+)/(\d+)\) (installing|upgrading|removing|reinstalling|downgrading) (\S+)`)
	pacmanTotalRe = regexp.MustCompile(`^(?:Packages|Targets) \((\d+)\)`)
)

// parsePacmanLine tracks "(n/N) upgrading pkg" lines and the "Packages (N)" header.
func parsePacmanLine(line string, p *Progress) bool {
	if g := pacmanTotalRe.FindStringSubmatch(line); g != nil {
		p.Total, _ = strconv.Atoi(g[1])
		p.Done = 0
		return true
	}
	if g := pacmanStepRe.FindStringSubmatch(line); g != nil {
		n, _ := strconv.Atoi(g[1])
		t, _ := strconv.Atoi(g[2])
		p.Done, p.Total, p.Current = n-1, t, g[4]
		return true
	}
	return false
}

func (p *pacman) step(title string, args ...string) Plan {
	return Plan{Steps: []Step{{Title: title, Name: "pacman", Args: args, Parse: parsePacmanLine}}}
}

func (p *pacman) Install(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return p.step("install", append([]string{"-S", "--noconfirm", "--needed", "--"}, pkgs...)...), nil
}

func (p *pacman) Remove(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return p.step("remove", append([]string{"-Rs", "--noconfirm", "--"}, pkgs...)...), nil
}

func (p *pacman) Upgrade(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	if len(pkgs) == 0 {
		return p.step("upgrade", "-Syu", "--noconfirm"), nil
	}
	return p.step("upgrade", append([]string{"-S", "--noconfirm", "--"}, pkgs...)...), nil
}

func orInvalid(err error) error {
	if err != nil {
		return err
	}
	return rpc.Errorf(rpc.Invalid, "Choose at least one package.")
}

// ---- AUR ----

// aur lists, searches and describes AUR packages through yay or paru. AUR
// builds must run as the unprivileged user and need sudo for the final
// install step, which the root bridge cannot provide, so installing and
// upgrading AUR packages is not supported from the web UI (removing is plain
// pacman and works).
type aur struct{ helper string }

func newAUR() *aur {
	for _, h := range []string{"yay", "paru"} {
		if have(h) {
			return &aur{helper: h}
		}
	}
	return &aur{}
}

func (a *aur) Name() string    { return a.helper }
func (a *aur) Kind() string    { return KindAUR }
func (a *aur) Available() bool { return a.helper != "" && !isRoot() }

func (a *aur) ListInstalled(context.Context) ([]Package, error) { return nil, nil }

func (a *aur) ListUpdates(ctx context.Context) ([]Update, error) {
	out, err := run(ctx, time.Minute, nil, a.helper, "-Qua")
	if err != nil && out == "" && exitCode(err) != 1 {
		return nil, err
	}
	var ups []Update
	for _, u := range parseQu(out) {
		u.Source, u.Kind = "aur", KindAUR
		ups = append(ups, u)
	}
	return ups, nil
}

func (a *aur) Search(ctx context.Context, q string) ([]Result, error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return nil, nil
	}
	out, err := run(ctx, 30*time.Second, nil, a.helper, append([]string{"-Ss", "--aur", "--"}, terms...)...)
	if err != nil && out == "" {
		return nil, err
	}
	res := parseSearch(out, KindAUR)
	for i := range res {
		res[i].Source = "aur"
	}
	return res, nil
}

func (a *aur) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	out, err := run(ctx, 30*time.Second, nil, a.helper, "-Si", "--aur", "--", name)
	if err != nil {
		return nil, rpc.Errorf(rpc.NotFound, "No AUR package called %q was found.", name)
	}
	f := parseKV(out)
	return &Detail{Name: name, Source: "aur", Kind: KindAUR, Version: fieldValue(f, "Version"), Description: fieldValue(f, "Description"), Fields: f}, nil
}

var errAURUnsupported = rpc.Errorf(rpc.Unavailable, "AUR packages are built as your own user and need an interactive sudo prompt, so they cannot be installed from here yet. Open a terminal and run the AUR helper (for example: yay -S <package>).")

func (a *aur) Install([]string) (Plan, error) { return Plan{}, errAURUnsupported }
func (a *aur) Upgrade([]string) (Plan, error) { return Plan{}, errAURUnsupported }
func (a *aur) Remove([]string) (Plan, error) {
	return Plan{}, rpc.Errorf(rpc.Internal, "AUR packages are removed with the system package manager")
}
func (a *aur) Refresh(context.Context) error { return nil }
