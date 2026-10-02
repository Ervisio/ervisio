package software

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// rpmInstalled lists installed packages through rpm (Fedora and openSUSE).
func rpmInstalled(ctx context.Context, source string, auto map[string]bool, autoKnown bool, userInstalled map[string]bool) ([]Package, error) {
	out, err := run(ctx, time.Minute, nil, "rpm", "-qa", "--qf", `%{NAME}\t%{VERSION}-%{RELEASE}\t%{SIZE}\t%{INSTALLTIME}\t%{SUMMARY}\n`)
	if err != nil {
		return nil, err
	}
	return parseRpmList(out, source, auto, autoKnown, userInstalled), nil
}

func parseRpmList(out, source string, auto map[string]bool, autoKnown bool, user map[string]bool) []Package {
	var pkgs []Package
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 5)
		if len(f) < 5 || f[0] == "gpg-pubkey" {
			continue
		}
		size, _ := strconv.ParseInt(f[2], 10, 64)
		date, _ := strconv.ParseInt(f[3], 10, 64)
		reason := "explicit"
		if auto[f[0]] || (user != nil && !user[f[0]]) {
			reason = "dependency"
		}
		pkgs = append(pkgs, Package{Name: f[0], Version: f[1], Source: source, Kind: KindRepo, Size: size, InstallDate: date, Reason: reason, Description: f[4]})
	}
	return pkgs
}

func rpmOwners(ctx context.Context, files []string) map[string]string {
	res := map[string]string{}
	for len(files) > 0 {
		n := len(files)
		if n > 100 {
			n = 100
		}
		out, _ := run(ctx, 30*time.Second, nil, "rpm", append([]string{"-qf", "--qf", `%{NAME}\n`, "--"}, files[:n]...)...)
		names := strings.Split(strings.TrimSpace(out), "\n")
		// rpm prints one line per file, including "file X is not owned by any package".
		if len(names) == n {
			for i, nm := range names {
				if !strings.Contains(nm, " ") {
					res[files[i]] = nm
				}
			}
		}
		files = files[n:]
	}
	return res
}

// currentVersions asks rpm for the installed version of names.
func currentVersions(ctx context.Context, names []string) map[string]string {
	m := map[string]string{}
	if len(names) == 0 {
		return m
	}
	out, _ := run(ctx, 30*time.Second, nil, "rpm", append([]string{"-q", "--qf", `%{NAME} %{VERSION}-%{RELEASE}\n`, "--"}, names...)...)
	for _, l := range strings.Split(out, "\n") {
		if n, v, ok := strings.Cut(l, " "); ok {
			m[n] = v
		}
	}
	return m
}

// ---- dnf ----

type dnf struct{}

func newDnf() *dnf { return &dnf{} }

func (d *dnf) Name() string    { return "dnf" }
func (d *dnf) Kind() string    { return KindRepo }
func (d *dnf) Available() bool { return have("dnf") && have("rpm") }

func (d *dnf) ListInstalled(ctx context.Context) ([]Package, error) {
	user := map[string]bool{}
	out, err := run(ctx, 90*time.Second, nil, "dnf", "-q", "repoquery", "--userinstalled", "--qf", `%{name}\n`)
	if err != nil || strings.TrimSpace(out) == "" {
		user = nil // unknown: everything counts as installed by the user
	} else {
		for _, n := range strings.Fields(out) {
			user[n] = true
		}
	}
	return rpmInstalled(ctx, "dnf", nil, false, user)
}

var dnfUpdRe = regexp.MustCompile(`^(\S+)\.(\w+)\s+(\S+)\s+(\S+)$`)

// parseDnfCheckUpdate parses `dnf check-update` ("name.arch  version  repo").
func parseDnfCheckUpdate(out string) []Update {
	var ups []Update
	for _, line := range strings.Split(out, "\n") {
		g := dnfUpdRe.FindStringSubmatch(strings.TrimSpace(line))
		if g == nil {
			continue
		}
		u := Update{Name: g[1], To: g[3], Source: g[4], Kind: KindRepo, Notes: []string{}}
		if needsReboot(u.Name) {
			u.Notes = append(u.Notes, "reboot")
		}
		ups = append(ups, u)
	}
	return ups
}

func (d *dnf) ListUpdates(ctx context.Context) ([]Update, error) {
	out, err := run(ctx, 3*time.Minute, nil, "dnf", "-q", "check-update")
	if err != nil && exitCode(err) != 100 {
		return nil, err
	}
	ups := parseDnfCheckUpdate(out)
	names := make([]string, len(ups))
	for i := range ups {
		names[i] = ups[i].Name
	}
	cur := currentVersions(ctx, names)
	for i := range ups {
		ups[i].From = cur[ups[i].Name]
	}
	if len(ups) > 0 {
		if s, err := run(ctx, 3*time.Minute, nil, "dnf", "-q", "check-update", "--security"); err == nil || exitCode(err) == 100 {
			sec := map[string]bool{}
			for _, u := range parseDnfCheckUpdate(s) {
				sec[u.Name] = true
			}
			for i := range ups {
				if sec[ups[i].Name] {
					ups[i].Notes = append(ups[i].Notes, "security")
				}
			}
		}
		if sz, err := run(ctx, 3*time.Minute, nil, "dnf", "-q", "repoquery", "--upgrades", "--qf", `%{name}|%{downloadsize}\n`); err == nil {
			sizes := map[string]int64{}
			for _, l := range strings.Split(sz, "\n") {
				if n, v, ok := strings.Cut(l, "|"); ok {
					x, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
					sizes[n] = x
				}
			}
			for i := range ups {
				ups[i].Size = sizes[ups[i].Name]
			}
		}
	}
	return ups, nil
}

func (d *dnf) Refresh(ctx context.Context) error {
	_, err := run(ctx, 3*time.Minute, nil, "dnf", "-q", "makecache")
	return err
}

// parseDnfSearch parses dnf4 `search` output ("name.arch : summary").
func parseDnfSearch(out string) []Result {
	var res []Result
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		n, desc, ok := strings.Cut(l, " : ")
		if !ok || strings.HasPrefix(l, "=") {
			continue
		}
		n = strings.TrimSpace(n)
		if i := strings.LastIndex(n, "."); i > 0 {
			n = n[:i]
		}
		if !nameRe.MatchString(n) || seen[n] {
			continue
		}
		seen[n] = true
		res = append(res, Result{Name: n, Source: "dnf", Kind: KindRepo, Description: strings.TrimSpace(desc)})
		if len(res) >= 40 {
			break
		}
	}
	return res
}

func (d *dnf) Search(ctx context.Context, q string) ([]Result, error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return nil, nil
	}
	out, err := run(ctx, time.Minute, nil, "dnf", append([]string{"-q", "search", "--"}, terms...)...)
	if err != nil && out == "" {
		return nil, err
	}
	res := parseDnfSearch(out)
	markInstalledRPM(ctx, res)
	return res, nil
}

func markInstalledRPM(ctx context.Context, res []Result) {
	if len(res) == 0 {
		return
	}
	names := make([]string, len(res))
	for i := range res {
		names[i] = res[i].Name
	}
	cur := currentVersions(ctx, names)
	for i := range res {
		if v, ok := cur[res[i].Name]; ok && !strings.Contains(v, "not installed") {
			res[i].Installed = true
			if res[i].Version == "" {
				res[i].Version = v
			}
		}
	}
}

func (d *dnf) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	out, err := run(ctx, time.Minute, nil, "dnf", "-q", "info", "--", name)
	if err != nil {
		return nil, rpc.Errorf(rpc.NotFound, "No package called %q was found.", name)
	}
	f := parseKV(out)
	det := &Detail{Name: name, Source: fieldValue(f, "From repository", "Repository"), Kind: KindRepo, Version: fieldValue(f, "Version"), Description: fieldValue(f, "Summary"), Fields: f}
	if cur := currentVersions(ctx, []string{name}); cur[name] != "" && !strings.Contains(cur[name], "not installed") {
		det.Installed = true
	}
	return det, nil
}

func (d *dnf) Owners(ctx context.Context, files []string) map[string]string {
	return rpmOwners(ctx, files)
}

func (d *dnf) Lookup(ctx context.Context, names []string) ([]Result, error) {
	var res []Result
	for _, n := range names {
		res = append(res, Result{Name: n, Source: "dnf", Kind: KindRepo})
	}
	out, _ := run(ctx, time.Minute, nil, "dnf", append([]string{"-q", "repoquery", "--latest-limit=1", "--qf", `%{name}|%{evr}|%{summary}\n`, "--"}, names...)...)
	var found []Result
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(l, "|", 3)
		if len(f) == 3 {
			found = append(found, Result{Name: f[0], Version: f[1], Description: f[2], Source: "dnf", Kind: KindRepo})
		}
	}
	markInstalledRPM(ctx, found)
	return found, nil
}

var (
	dnfStepRe  = regexp.MustCompile(`^\s*(Installing|Upgrading|Erasing|Removing|Reinstalling|Downgrading)\s*:\s*(\S+)\s+(\d+)/(\d+)\s*$`)
	dnf5StepRe = regexp.MustCompile(`^\[(\d+)/(\d+)\]\s+(Installing|Upgrading|Erasing|Removing|Reinstalling|Downgrading)\s+(\S+)`)
)

// parseDnfLine understands both dnf4 ("Upgrading : pkg 3/42") and dnf5 ("[3/42] Upgrading pkg").
func parseDnfLine(line string, p *Progress) bool {
	if g := dnfStepRe.FindStringSubmatch(line); g != nil {
		n, _ := strconv.Atoi(g[3])
		t, _ := strconv.Atoi(g[4])
		p.Done, p.Total, p.Current = n-1, t, g[2]
		return true
	}
	if g := dnf5StepRe.FindStringSubmatch(line); g != nil {
		n, _ := strconv.Atoi(g[1])
		t, _ := strconv.Atoi(g[2])
		p.Done, p.Total, p.Current = n-1, t, g[4]
		return true
	}
	return false
}

func (d *dnf) step(title string, args ...string) Plan {
	return Plan{Steps: []Step{{Title: title, Name: "dnf", Args: append([]string{"-y"}, args...), Parse: parseDnfLine}}}
}

func (d *dnf) Install(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return d.step("install", append([]string{"install", "--"}, pkgs...)...), nil
}

func (d *dnf) Remove(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return d.step("remove", append([]string{"remove", "--"}, pkgs...)...), nil
}

func (d *dnf) Upgrade(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	if len(pkgs) == 0 {
		return d.step("upgrade", "upgrade", "--refresh"), nil
	}
	return d.step("upgrade", append([]string{"upgrade", "--"}, pkgs...)...), nil
}

// ---- zypper ----

type zypper struct{}

func newZypper() *zypper { return &zypper{} }

func (z *zypper) Name() string    { return "zypper" }
func (z *zypper) Kind() string    { return KindRepo }
func (z *zypper) Available() bool { return have("zypper") && have("rpm") }

func (z *zypper) ListInstalled(ctx context.Context) ([]Package, error) {
	auto := map[string]bool{}
	if f, err := os.Open("/var/lib/zypp/AutoInstalled"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			auto[strings.TrimSpace(sc.Text())] = true
		}
	}
	return rpmInstalled(ctx, "zypper", auto, true, nil)
}

// parseZypperTable parses the pipe separated tables of zypper -q output,
// skipping the header and separator.
func parseZypperTable(out string) [][]string {
	var rows [][]string
	seenSep := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "--") {
			seenSep = true
			continue
		}
		if !seenSep || !strings.Contains(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		rows = append(rows, parts)
	}
	return rows
}

func parseZypperUpdates(out string) []Update {
	var ups []Update
	for _, r := range parseZypperTable(out) {
		if len(r) < 5 {
			continue
		}
		u := Update{Name: r[2], From: r[3], To: r[4], Source: r[1], Kind: KindRepo, Notes: []string{}}
		if needsReboot(u.Name) {
			u.Notes = append(u.Notes, "reboot")
		}
		ups = append(ups, u)
	}
	return ups
}

func (z *zypper) ListUpdates(ctx context.Context) ([]Update, error) {
	out, err := run(ctx, 3*time.Minute, nil, "zypper", "-q", "--non-interactive", "list-updates")
	if err != nil && out == "" {
		return nil, err
	}
	return parseZypperUpdates(out), nil
}

func (z *zypper) Refresh(ctx context.Context) error {
	if !isRoot() {
		return nil
	}
	_, err := run(ctx, 3*time.Minute, nil, "zypper", "-q", "--non-interactive", "refresh")
	return err
}

func parseZypperSearch(out string) []Result {
	var res []Result
	for _, r := range parseZypperTable(out) {
		if len(r) < 3 || !nameRe.MatchString(r[1]) || (len(r) > 3 && r[3] != "package") {
			continue
		}
		res = append(res, Result{Name: r[1], Description: r[2], Source: "zypper", Kind: KindRepo, Installed: strings.Contains(r[0], "i")})
		if len(res) >= 40 {
			break
		}
	}
	return res
}

func (z *zypper) Search(ctx context.Context, q string) ([]Result, error) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return nil, nil
	}
	out, err := run(ctx, time.Minute, nil, "zypper", append([]string{"-q", "--non-interactive", "search", "--"}, terms...)...)
	if err != nil && out == "" && exitCode(err) != 104 {
		return nil, err
	}
	return parseZypperSearch(out), nil
}

func (z *zypper) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	out, err := run(ctx, time.Minute, nil, "zypper", "-q", "--non-interactive", "info", "--", name)
	if err != nil {
		return nil, rpc.Errorf(rpc.NotFound, "No package called %q was found.", name)
	}
	f := parseKV(out)
	return &Detail{Name: name, Source: fieldValue(f, "Repository"), Kind: KindRepo, Installed: strings.EqualFold(fieldValue(f, "Installed"), "yes"),
		Version: fieldValue(f, "Version"), Description: fieldValue(f, "Summary"), Fields: f}, nil
}

func (z *zypper) Owners(ctx context.Context, files []string) map[string]string {
	return rpmOwners(ctx, files)
}

var zypperStepRe = regexp.MustCompile(`^\((\d+)/(\d+)\) (Installing|Removing|Updating|Upgrading): (\S+)`)

func parseZypperLine(line string, p *Progress) bool {
	if g := zypperStepRe.FindStringSubmatch(line); g != nil {
		n, _ := strconv.Atoi(g[1])
		t, _ := strconv.Atoi(g[2])
		p.Done, p.Total, p.Current = n-1, t, g[4]
		return true
	}
	return false
}

func (z *zypper) step(title string, args ...string) Step {
	return Step{Title: title, Name: "zypper", Args: append([]string{"--non-interactive"}, args...), Parse: parseZypperLine}
}

func (z *zypper) Install(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return Plan{Steps: []Step{z.step("install", append([]string{"install", "--"}, pkgs...)...)}}, nil
}

func (z *zypper) Remove(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil || len(pkgs) == 0 {
		return Plan{}, orInvalid(err)
	}
	return Plan{Steps: []Step{z.step("remove", append([]string{"remove", "--"}, pkgs...)...)}}, nil
}

func (z *zypper) Upgrade(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	if len(pkgs) == 0 {
		return Plan{Steps: []Step{z.step("refresh", "refresh"), z.step("upgrade", "update")}}, nil
	}
	return Plan{Steps: []Step{z.step("upgrade", append([]string{"update", "--"}, pkgs...)...)}}, nil
}
