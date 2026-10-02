package software

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// flatpak manages Flatpak apps in the system and the user installation.
type flatpak struct {
	scope string // "" = both when reading, "system" or "user"
}

func newFlatpak() *flatpak { return &flatpak{} }

func (f *flatpak) Name() string    { return "flatpak" }
func (f *flatpak) Kind() string    { return KindFlatpak }
func (f *flatpak) Available() bool { return have("flatpak") }

// WithScope returns a copy that acts on one installation.
func (f *flatpak) WithScope(scope string) Backend { return &flatpak{scope: scope} }

func (f *flatpak) scopes() []string {
	if f.scope != "" {
		return []string{f.scope}
	}
	return []string{"system", "user"}
}

// parseFlatpakList parses `flatpak list --app --columns=application,name,version,branch,origin,installation,size,description`.
func parseFlatpakList(out string) []Package {
	var pkgs []Package
	for _, line := range strings.Split(out, "\n") {
		c := strings.Split(line, "\t")
		if len(c) < 7 || c[0] == "" {
			continue
		}
		p := Package{Name: c[0], Title: c[1], Version: c[2], Source: "flatpak", Kind: KindFlatpak, Reason: "explicit", Size: parseHumanSize(c[6]), Scope: c[5]}
		if p.Version == "" {
			p.Version = c[3] // the branch (stable)
		}
		if len(c) > 7 {
			p.Description = c[7]
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func (f *flatpak) ListInstalled(ctx context.Context) ([]Package, error) {
	out, err := run(ctx, time.Minute, nil, "flatpak", "list", "--app", "--columns=application,name,version,branch,origin,installation,size,description")
	if err != nil {
		return nil, err
	}
	return parseFlatpakList(out), nil
}

// parseFlatpakUpdates parses remote-ls --updates with columns application,name,version,origin,download-size.
func parseFlatpakUpdates(out, scope string, current map[string]string) []Update {
	var ups []Update
	for _, line := range strings.Split(out, "\n") {
		c := strings.Split(line, "\t")
		if len(c) < 5 || c[0] == "" {
			continue
		}
		u := Update{Name: c[0], Title: c[1], To: c[2], From: current[scope+"/"+c[0]], Source: "flatpak", Kind: KindFlatpak, Size: parseHumanSize(c[4]), Notes: []string{}, Scope: scope}
		if u.To == "" {
			u.To = "latest"
		}
		ups = append(ups, u)
	}
	return ups
}

func (f *flatpak) ListUpdates(ctx context.Context) ([]Update, error) {
	cur := map[string]string{}
	if out, err := run(ctx, time.Minute, nil, "flatpak", "list", "--columns=application,version,installation"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			c := strings.Split(l, "\t")
			if len(c) >= 3 {
				cur[c[2]+"/"+c[0]] = c[1]
			}
		}
	}
	var all []Update
	var firstErr error
	for _, scope := range f.scopes() {
		out, err := run(ctx, 2*time.Minute, nil, "flatpak", "remote-ls", "--"+scope, "--updates", "--columns=application,name,version,origin,download-size")
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		all = append(all, parseFlatpakUpdates(out, scope, cur)...)
	}
	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func (f *flatpak) Refresh(ctx context.Context) error { return nil } // remote-ls reads fresh metadata

// parseFlatpakSearch parses `flatpak search --columns=name,description,application,version,remotes`.
func parseFlatpakSearch(out string) []Result {
	var res []Result
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		c := strings.Split(line, "\t")
		if len(c) < 5 || c[2] == "" || seen[c[2]] {
			continue
		}
		seen[c[2]] = true
		remote, _, _ := strings.Cut(c[4], ",")
		res = append(res, Result{Name: c[2], Title: c[0], Description: c[1], Version: c[3], Source: "flatpak", Kind: KindFlatpak, Remote: remote})
		if len(res) >= 40 {
			break
		}
	}
	return res
}

func (f *flatpak) installedSet(ctx context.Context) map[string]bool {
	set := map[string]bool{}
	if out, err := run(ctx, 30*time.Second, nil, "flatpak", "list", "--columns=application"); err == nil {
		for _, n := range strings.Fields(out) {
			set[n] = true
		}
	}
	return set
}

func (f *flatpak) Search(ctx context.Context, q string) ([]Result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	out, err := run(ctx, time.Minute, nil, "flatpak", "search", "--columns=name,description,application,version,remotes", "--", q)
	if err != nil && out == "" {
		return nil, err
	}
	res := parseFlatpakSearch(out)
	set := f.installedSet(ctx)
	for i := range res {
		res[i].Installed = set[res[i].Name]
	}
	return res, nil
}

// Lookup resolves exact app ids from the remote catalogue.
func (f *flatpak) Lookup(ctx context.Context, ids []string) ([]Result, error) {
	out, err := run(ctx, time.Minute, nil, "flatpak", "remote-ls", "--app", "--columns=application,name,description,version,origin")
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	set := f.installedSet(ctx)
	var res []Result
	for _, line := range strings.Split(out, "\n") {
		c := strings.Split(line, "\t")
		if len(c) >= 5 && want[c[0]] {
			res = append(res, Result{Name: c[0], Title: c[1], Description: c[2], Version: c[3], Source: "flatpak", Kind: KindFlatpak, Remote: c[4], Installed: set[c[0]]})
			delete(want, c[0])
		}
	}
	return res, nil
}

func (f *flatpak) Info(ctx context.Context, name string) (*Detail, error) {
	if err := validNames([]string{name}); err != nil {
		return nil, err
	}
	installed := true
	out, err := run(ctx, 30*time.Second, nil, "flatpak", "info", "--", name)
	if err != nil {
		installed = false
		remote := "flathub"
		if r, e := run(ctx, 10*time.Second, nil, "flatpak", "remotes", "--columns=name"); e == nil {
			if fs := strings.Fields(r); len(fs) > 0 {
				remote = fs[0]
			}
		}
		out, err = run(ctx, 60*time.Second, nil, "flatpak", "remote-info", remote, "--", name)
		if err != nil {
			return nil, rpc.Errorf(rpc.NotFound, "No Flatpak app called %q was found.", name)
		}
	}
	// The first line is "Title - Summary"; the rest is "Key: value".
	lines := strings.Split(strings.TrimLeft(out, "\n"), "\n")
	title, summary := "", ""
	if len(lines) > 0 {
		title, summary, _ = strings.Cut(strings.TrimSpace(lines[0]), " - ")
		if !strings.Contains(lines[0], " - ") {
			title = ""
		}
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i]) // flatpak right-aligns the keys
	}
	fl := parseKV(strings.TrimLeft(strings.Join(lines[1:], "\n"), "\n"))
	d := &Detail{Name: name, Source: "flatpak", Kind: KindFlatpak, Installed: installed, Version: fieldValue(fl, "Version"), Description: summary, Fields: fl}
	if title != "" {
		d.Fields = append([]Field{{"Name", title}}, d.Fields...)
	}
	return d, nil
}

func (f *flatpak) Owners(ctx context.Context, files []string) map[string]string { return nil }

var flatpakStepRe = regexp.MustCompile(`(Installing|Updating|Uninstalling)(?: \(|\s+)(\d+)/(\d+)`)

// parseFlatpakLine reads "Updating 2/5…" progress lines.
func parseFlatpakLine(line string, p *Progress) bool {
	if g := flatpakStepRe.FindStringSubmatch(line); g != nil {
		n, _ := strconv.Atoi(g[2])
		t, _ := strconv.Atoi(g[3])
		p.Done, p.Total = n-1, t
		return true
	}
	return false
}

func (f *flatpak) args(verb string, extra ...string) []string {
	a := []string{verb, "-y", "--noninteractive"}
	if f.scope != "" {
		a = append(a, "--"+f.scope)
	}
	return append(a, extra...)
}

func (f *flatpak) step(title string, args []string) Plan {
	return Plan{Steps: []Step{{Title: title, Name: "flatpak", Args: args, Parse: parseFlatpakLine}}}
}

// splitRef separates an optional "remote/app.id" reference.
func splitRef(s string) (remote, id string) {
	if i := strings.Index(s, "/"); i > 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

func (f *flatpak) validRefs(refs []string) error {
	for _, r := range refs {
		remote, id := splitRef(r)
		list := []string{id}
		if remote != "" {
			list = append(list, remote)
		}
		if err := validNames(list); err != nil {
			return err
		}
	}
	return nil
}

func (f *flatpak) Install(pkgs []string) (Plan, error) {
	if len(pkgs) == 0 {
		return Plan{}, orInvalid(nil)
	}
	if err := f.validRefs(pkgs); err != nil {
		return Plan{}, err
	}
	var steps []Step
	for _, r := range pkgs {
		remote, id := splitRef(r)
		var extra []string
		if remote != "" {
			extra = append(extra, remote)
		}
		extra = append(extra, id)
		steps = append(steps, Step{Title: "install", Name: "flatpak", Args: f.args("install", extra...), Parse: parseFlatpakLine})
	}
	return Plan{Steps: steps}, nil
}

func (f *flatpak) Remove(pkgs []string) (Plan, error) {
	if len(pkgs) == 0 {
		return Plan{}, orInvalid(nil)
	}
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	return f.step("remove", f.args("uninstall", pkgs...)), nil
}

func (f *flatpak) Upgrade(pkgs []string) (Plan, error) {
	if err := validNames(pkgs); err != nil {
		return Plan{}, err
	}
	return f.step("upgrade", f.args("update", pkgs...)), nil
}
