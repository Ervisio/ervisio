package software

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// apt covers Debian, Ubuntu and derivatives.
type apt struct{}

func newApt() *apt { return &apt{} }

func (a *apt) Name() string    { return "apt" }
func (a *apt) Kind() string    { return KindRepo }
func (a *apt) Available() bool { return have("apt-get") && have("dpkg-query") }

var aptEnv = []string{"DEBIAN_FRONTEND=noninteractive", "APT_LISTCHANGES_FRONTEND=none", "NEEDRESTART_MODE=a"}

// parseDpkgList parses dpkg-query -W with the format used by ListInstalled:
// name \t version \t size(KiB) \t status \t summary.
func parseDpkgList(out string, manual map[string]bool, orphans map[string]bool) []Package {
	var pkgs []Package
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 5 || !strings.HasPrefix(f[3], "ii") {
			continue
		}
		kib, _ := strconv.ParseInt(strings.TrimSpace(f[2]), 10, 64)
		reason := "dependency"
		if manual[f[0]] {
			reason = "explicit"
		}
		pkgs = append(pkgs, Package{Name: f[0], Version: f[1], Source: "apt", Kind: KindRepo, Size: kib * 1024,
			Reason: reason, Description: f[4], Orphan: orphans[f[0]]})
	}
	return pkgs
}

var aptRemvRe = regexp.MustCompile(`^Remv (\S+)`)

func (a *apt) ListInstalled(ctx context.Context) ([]Package, error) {
	out, err := run(ctx, time.Minute, nil, "dpkg-query", "-W", "-f", "${Package}\t${Version}\t${Installed-Size}\t${db:Status-Abbrev}\t${binary:Summary}\n")
	if err != nil {
		return nil, err
	}
	manual := map[string]bool{}
	if m, err := run(ctx, 30*time.Second, nil, "apt-mark", "showmanual"); err == nil {
		for _, n := range strings.Fields(m) {
			manual[n] = true
		}
	}
	orphans := map[string]bool{}
	if s, _ := run(ctx, 30*time.Second, aptEnv, "apt-get", "-s", "autoremove"); s != "" {
		for _, l := range strings.Split(s, "\n") {
			if g := aptRemvRe.FindStringSubmatch(l); g != nil {
				orphans[g[1]] = true
			}
		}
	}
	pkgs := parseDpkgList(out, manual, orphans)
	for i := range pkgs {
		if fi, err := os.Stat(filepath.Join("/var/lib/dpkg/info", pkgs[i].Name+".list")); err == nil {
			pkgs[i].InstallDate = fi.ModTime().Unix()
		}
	}
	return pkgs, nil
}

var aptUpgradableRe = regexp.MustCompile(`^(\S+?)/(\S+) (\S+) \S+ \[upgradable from: ([^\]]+)\]`)

// parseAptUpgradable parses `apt list --upgradable`.
func parseAptUpgradable(out string) []Update {
	var ups []Update
	for _, line := range strings.Split(out, "\n") {
		g := aptUpgradableRe.FindStringSubmatch(strings.TrimSpace(line))
		if g == nil {
			continue
		}
		origins := strings.Split(g[2], ",")
		u := Update{Name: g[1], To: g[3], From: g[4], Source: origins[0], Kind: KindRepo, Notes: []string{}}
		for _, o := range origins {
			if strings.Contains(o, "security") {
				u.Notes = append(u.Notes, "security")
				break
			}
		}
		if needsReboot(u.Name) {
			u.Notes = append(u.Notes, "reboot")
		}
		ups = append(ups, u)
	}
	return ups
}

var aptURIRe = regexp.MustCompile(`^'[^']+' (\S+?)_\S+ (\d+) `)

// parseAptURIs reads `apt-get --print-uris` into package -> download size.
func parseAptURIs(out string) map[string]int64 {
	m := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		if g := aptURIRe.FindStringSubmatch(line); g != nil {
			n, _ := strconv.ParseInt(g[2], 10, 64)
			m[g[1]] += n
		}
	}
	return m
}

func (a *apt) ListUpdates(ctx context.Context) ([]Update, error) {
	out, err := run(ctx, time.Minute, aptEnv, "apt", "list", "--upgradable")
	if err != nil && out == "" {
		return nil, err
	}
	ups := parseAptUpgradable(out)
	if len(ups) > 0 {
		if uris, _ := run(ctx, time.Minute, aptEnv, "apt-get", "--print-uris", "-qq", "-y", "dist-upgrade"); uris != "" {
			sizes := parseAptURIs(uris)
			for i := range ups {
				ups[i].Size = sizes[ups[i].Name]
			}
		}
	}
	return ups, nil
}

func (a *apt) Refresh(ctx context.Context) error {
	if !isRoot() {
		return nil
	}
	_, err := run(ctx, 3*time.Minute, aptEnv, "apt-get", "update", "-qq")
	return err
}

// parseAptPolicy parses `apt-cache policy` for several packages.
func parseAptPolicy(out string) map[string][2]string {
	m := map[string][2]string{}
	var cur string
	var inst, cand string
	flush := func() {
		if cur != "" {
			m[cur] = [2]string{inst, cand}
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if line != "" && line[0] != ' ' && strings.HasSuffix(line, ":") {
			flush()
			cur, inst, cand = strings.TrimSuffix(line, ":"), "", ""
			continue
		}
		t := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(t, "Installed:"); ok {
			inst = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(t, "Candidate:"); ok {
			cand = strings.TrimSpace(v)
		}
	}
	flush()
	return m
}

func (a *apt) Search(ctx context.Context, q string) ([]Result, error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return nil, nil
	}
	out, err := run(ctx, 30*time.Second, nil, "apt-cache", append([]string{"search", "--names-only", "--"}, terms...)...)
	if err != nil && out == "" && exitCode(err) != 1 {
		return nil, err
	}
	var res []Result
	for _, l := range strings.Split(out, "\n") {
		n, d, ok := strings.Cut(l, " - ")
		if !ok || !nameRe.MatchString(n) {
			continue
		}
		res = append(res, Result{Name: n, Source: "apt", Kind: KindRepo, Description: d})
		if len(res) >= 40 {
			break
		}
	}
	a.fill(ctx, res)
	return res, nil
}

func (a *apt) fill(ctx context.Context, res []Result) {
	if len(res) == 0 {
		return
	}
	names := make([]string, len(res))
	for i := range res {
		names[i] = res[i].Name
	}
	if out, err := run(ctx, 20*time.Second, nil, "apt-cache", append([]string{"policy", "--"}, names...)...); err == nil {
		pol := parseAptPolicy(out)
		for i := range res {
			if v, ok := pol[res[i].Name]; ok {
				res[i].Version = v[1]
				res[i].Installed = v[0] != "" && v[0] != "(none)"
			}
		}
	}
}

func (a *apt) Lookup(ctx context.Context, names []string) ([]Result, error) {
	var res []Result
	for _, n := range names {
		res = append(res, Result{Name: n, Source: "apt", Kind: KindRepo})
	}
	a.fill(ctx, res)
	var out []Result
	for _, r := range res {
		if r.Version != "" && r.Version != "(none)" {
			if d, err := run(ctx, 10*time.Second, nil, "apt-cache", "show", "--no-all-versions", "--", r.Name); err == nil {
				r.Description = fieldValue(parseKV(d), "Description")
			}
			out = append(out, r)
		}
	}
	return out, nil
}

func (a *apt) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	out, err := run(ctx, 20*time.Second, nil, "apt-cache", "show", "--no-all-versions", "--", name)
	if err != nil || strings.TrimSpace(out) == "" {
		return nil, rpc.Errorf(rpc.NotFound, "No package called %q was found.", name)
	}
	f := parseKV(out)
	d := &Detail{Name: name, Source: "apt", Kind: KindRepo, Version: fieldValue(f, "Version"), Description: fieldValue(f, "Description"), Fields: f}
	if st, err := run(ctx, 10*time.Second, nil, "dpkg-query", "-W", "-f", "${db:Status-Abbrev}", name); err == nil && strings.HasPrefix(st, "ii") {
		d.Installed = true
	}
	return d, nil
}

func (a *apt) Owners(ctx context.Context, files []string) map[string]string {
	res := map[string]string{}
	for _, f := range files {
		out, err := run(ctx, 10*time.Second, nil, "dpkg", "-S", "--", f)
		if err != nil {
			continue
		}
		if pkg, _, ok := strings.Cut(out, ": "); ok {
			pkg, _, _ = strings.Cut(pkg, ",")
			pkg, _, _ = strings.Cut(pkg, ":")
			res[f] = strings.TrimSpace(pkg)
		}
	}
	return res
}

var (
	aptCountRe = regexp.MustCompile(`^(\d+) upgraded, (\d+) newly installed, (\d+) to remove`)
	aptUnpkRe  = regexp.MustCompile(`^Unpacking (\S+?)(?::\S+)? \(`)
	aptSetupRe = regexp.MustCompile(`^Setting up (\S+?)(?::\S+)? \(`)
	aptRemRe   = regexp.MustCompile(`^Removing (\S+?)(?::\S+)? \(`)
)

// parseAptLine counts packages configured (or removed) and notes the current one.
func parseAptLine(line string, p *Progress) bool {
	if g := aptCountRe.FindStringSubmatch(line); g != nil {
		a, _ := strconv.Atoi(g[1])
		b, _ := strconv.Atoi(g[2])
		c, _ := strconv.Atoi(g[3])
		p.Total, p.Done = a+b+c, 0
		return true
	}
	if g := aptUnpkRe.FindStringSubmatch(line); g != nil {
		p.Current = g[1]
		return true
	}
	if g := aptSetupRe.FindStringSubmatch(line); g != nil {
		p.Current = g[1]
		p.Done++
		return true
	}
	if g := aptRemRe.FindStringSubmatch(line); g != nil {
		p.Current = g[1]
		p.Done++
		return true
	}
	return false
}

func aptStep(title string, args ...string) Step {
	opts := []string{"-y", "-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Options::=--force-confdef"}
	return Step{Title: title, Name: "apt-get", Args: append(opts, args...), Env: aptEnv, Parse: parseAptLine}
}

func (a *apt) Install(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return Plan{Steps: []Step{aptStep("install", append([]string{"install", "--"}, pkgs...)...)}}, nil
}

func (a *apt) Remove(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return Plan{Steps: []Step{aptStep("remove", append([]string{"remove", "--"}, pkgs...)...)}}, nil
}

func (a *apt) Upgrade(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	if len(pkgs) == 0 {
		return Plan{Steps: []Step{
			{Title: "refresh", Name: "apt-get", Args: []string{"update"}, Env: aptEnv},
			aptStep("upgrade", "dist-upgrade"),
		}}, nil
	}
	return Plan{Steps: []Step{aptStep("upgrade", append([]string{"install", "--only-upgrade", "--"}, pkgs...)...)}}, nil
}
