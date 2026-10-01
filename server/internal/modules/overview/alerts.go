package overview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/sys"
)

const (
	updatesTTL = 10 * time.Minute
	sshTTL     = 5 * time.Minute
	maxFailed  = 6
)

// Action tells the web app where an alert leads: /<section>?<params>.
type Action struct {
	Section string            `json:"section"`
	Params  map[string]string `json:"params,omitempty"`
}

// Alert is one thing that needs attention (or, with severity ok, a reassurance).
// The web app translates TitleKey (namespace overview, key alerts.<TitleKey>)
// with Vars; Title and Detail are English fallbacks.
type Alert struct {
	ID       string         `json:"id"`
	Severity string         `json:"severity"` // ok | warn | err
	TitleKey string         `json:"titleKey,omitempty"`
	Title    string         `json:"title"`
	Vars     map[string]any `json:"vars,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Action   *Action        `json:"action,omitempty"`
}

type cached struct {
	at    time.Time
	alert []Alert
}

type alerter struct {
	mu    sync.Mutex
	cache map[string]cached
}

func newAlerter() *alerter { return &alerter{cache: map[string]cached{}} }

func (a *alerter) cached(key string, ttl time.Duration, fn func() []Alert) []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.cache[key]; ok && time.Since(c.at) < ttl {
		return c.alert
	}
	v := fn()
	a.cache[key] = cached{time.Now(), v}
	return v
}

var severityRank = map[string]int{"err": 0, "warn": 1, "ok": 2}

// collect gathers every alert. A check that cannot run is left out.
func (a *alerter) collect(ctx context.Context) []Alert {
	out := []Alert{}
	out = append(out, failedUnits(ctx)...)
	out = append(out, a.cached("updates", updatesTTL, func() []Alert { return updatesAlert(ctx) })...)
	out = append(out, a.cached("ssh", sshTTL, func() []Alert { return sshAlert(ctx) })...)
	out = append(out, diskAlerts()...)
	out = append(out, swapAlert()...)
	sort.SliceStable(out, func(i, j int) bool { return severityRank[out[i].Severity] < severityRank[out[j].Severity] })
	return out
}

/* ---------- failed units ---------- */

type unitState struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

func failedUnits(ctx context.Context) []Alert {
	out, err := sys.Output(ctx, "systemctl", "list-units", "--state=failed", "--output=json", "--no-pager", "--all")
	if err != nil {
		return nil
	}
	return failedAlerts(out, func(unit string) string { return failedSince(ctx, unit) })
}

// failedAlerts turns the JSON of `systemctl list-units --state=failed --output=json`
// into alerts. since is optional and returns a human timestamp for a unit.
func failedAlerts(js []byte, since func(string) string) []Alert {
	units, err := parseUnits(js)
	if err != nil {
		return nil
	}
	var out []Alert
	for i, u := range units {
		if i >= maxFailed {
			out = append(out, Alert{
				ID: "units-more", Severity: "err", TitleKey: "moreFailed",
				Title: fmt.Sprintf("%d more units failed", len(units)-maxFailed), Vars: map[string]any{"count": len(units) - maxFailed},
				Action: &Action{Section: "services", Params: map[string]string{"state": "failed"}},
			})
			break
		}
		detail := u.Description
		if since != nil {
			if s := since(u.Unit); s != "" {
				detail = s
			}
		}
		out = append(out, Alert{
			ID: "unit:" + u.Unit, Severity: "err", TitleKey: "failedUnit",
			Title: u.Unit + " failed", Vars: map[string]any{"unit": u.Unit},
			Detail: detail, Action: &Action{Section: "services", Params: map[string]string{"unit": u.Unit}},
		})
	}
	return out
}

func failedSince(ctx context.Context, unit string) string {
	out, err := sys.Output(ctx, "systemctl", "show", unit, "--property=ActiveEnterTimestamp,InactiveEnterTimestamp,Result,ExecMainStatus")
	if err != nil {
		return ""
	}
	return describeFailure(string(out))
}

// describeFailure builds "Failed at <time> (<result>)" from `systemctl show` output.
func describeFailure(show string) string {
	kv := map[string]string{}
	for _, l := range strings.Split(show, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	ts := kv["InactiveEnterTimestamp"]
	if ts == "" || ts == "n/a" {
		ts = kv["ActiveEnterTimestamp"]
	}
	res := kv["Result"]
	switch {
	case ts != "" && ts != "n/a" && res != "":
		return fmt.Sprintf("Failed %s (%s)", ts, res)
	case res != "":
		return "Failed: " + res
	}
	return ""
}

func parseUnits(js []byte) ([]unitState, error) {
	js = bytes.TrimSpace(js)
	if len(js) == 0 {
		return nil, nil
	}
	var units []unitState
	if err := json.Unmarshal(js, &units); err != nil {
		return nil, err
	}
	return units, nil
}

/* ---------- pending updates ---------- */

func updatesAlert(ctx context.Context) []Alert {
	pkgs, ok := pendingUpdates(ctx)
	if !ok {
		return nil
	}
	return updatesFrom(pkgs)
}

func updatesFrom(pkgs []string) []Alert {
	if len(pkgs) == 0 {
		return []Alert{{ID: "updates", Severity: "ok", TitleKey: "upToDate", Title: "System is up to date",
			Action: &Action{Section: "software"}}}
	}
	show := pkgs
	if len(show) > 3 {
		show = show[:3]
	}
	return []Alert{{
		ID: "updates", Severity: "warn", TitleKey: "updates", Title: fmt.Sprintf("%d updates ready", len(pkgs)),
		Vars:   map[string]any{"count": len(pkgs)},
		Detail: strings.Join(show, ", "), Action: &Action{Section: "software"},
	}}
}

// pendingUpdates lists "name version" for each pending update. ok is false
// when no package manager could be queried. It never refreshes from the network.
func pendingUpdates(ctx context.Context) ([]string, bool) {
	has := func(n string) bool { _, err := sys.LookPath(n); return err == nil }
	// checkupdates is deliberately not used: it downloads fresh databases.
	// pacman -Qu answers from the databases the system last synced.
	switch {
	case has("pacman"):
		out, err := sys.Output(ctx, "pacman", "-Qu")
		if err != nil {
			if ee, isExit := err.(*sys.ExitError); isExit && ee.Code == 1 && len(out) == 0 {
				return nil, true // nothing to upgrade
			}
			return nil, false
		}
		return parseArchUpdates(out), true
	case has("apt"):
		out, err := sys.Output(ctx, "apt", "list", "--upgradable")
		if err != nil {
			return nil, false
		}
		return parseAptUpdates(out), true
	case has("dnf"):
		out, err := sys.Output(ctx, "dnf", "check-update", "--quiet", "--cacheonly")
		if err != nil {
			if ee, isExit := err.(*sys.ExitError); isExit && ee.Code == 100 {
				return parseDnfUpdates(out), true
			}
			return nil, false
		}
		return parseDnfUpdates(out), true
	}
	return nil, false
}

// parseArchUpdates parses "name old -> new" lines (checkupdates, pacman -Qu).
func parseArchUpdates(out []byte) []string {
	var pkgs []string
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 4 && f[2] == "->" {
			pkgs = append(pkgs, f[0]+" "+f[3])
		} else if len(f) >= 1 && len(f) < 4 && f[0] != "" && !strings.HasPrefix(f[0], ":") {
			pkgs = append(pkgs, f[0])
		}
	}
	return pkgs
}

// parseAptUpdates parses `apt list --upgradable` ("name/suite new arch [upgradable from: old]").
func parseAptUpdates(out []byte) []string {
	var pkgs []string
	for _, l := range strings.Split(string(out), "\n") {
		if !strings.Contains(l, "[upgradable") {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		name, _, _ := strings.Cut(f[0], "/")
		pkgs = append(pkgs, name+" "+f[1])
	}
	return pkgs
}

// parseDnfUpdates parses `dnf check-update` ("name.arch version repo").
func parseDnfUpdates(out []byte) []string {
	var pkgs []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "Obsoleting") {
			break
		}
		f := strings.Fields(l)
		if len(f) != 3 {
			continue
		}
		name, _, _ := strings.Cut(f[0], ".")
		pkgs = append(pkgs, name+" "+f[1])
	}
	return pkgs
}

/* ---------- SSH failed logins ---------- */

func sshAlert(ctx context.Context) []Alert {
	if _, err := sys.LookPath("sshd"); err != nil {
		return nil // no SSH server on this machine
	}
	out, err := sys.Cmd{Name: "journalctl", Args: []string{"--no-pager", "-q", "-o", "cat", "--since", "24 hours ago", "-t", "sshd", "-t", "sshd-session"}, Timeout: 15 * time.Second}.Output(ctx)
	if err != nil {
		return nil // not readable by this user, or no journal
	}
	n := countSSHFailures(out)
	if n == 0 {
		return []Alert{{ID: "ssh", Severity: "ok", TitleKey: "noSSH", Title: "No failed SSH logins in the last 24 hours",
			Detail: "Checked at " + time.Now().Format("15:04")}}
	}
	return []Alert{{ID: "ssh", Severity: "warn", TitleKey: "sshFailed", Title: fmt.Sprintf("%d failed SSH logins in the last 24 hours", n),
		Vars: map[string]any{"count": n}, Action: &Action{Section: "logs", Params: map[string]string{"q": "Failed password"}}}}
}

func countSSHFailures(out []byte) int {
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "Failed password") || strings.HasPrefix(l, "Invalid user") ||
			strings.Contains(l, "authentication failure") || strings.HasPrefix(l, "Failed publickey") {
			n++
		}
	}
	return n
}

/* ---------- disks and swap ---------- */

type mount struct{ dev, point, fstype string }

var realFS = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "btrfs": true, "xfs": true, "vfat": true, "exfat": true,
	"ntfs": true, "ntfs3": true, "f2fs": true, "zfs": true, "nfs": true, "nfs4": true, "jfs": true, "reiserfs": true}

func parseMounts(r io.Reader) []mount {
	var out []mount
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		if strings.HasPrefix(f[0], "/") {
			seen[f[0]] = true // one entry per device (btrfs subvolumes)
		}
		out = append(out, mount{f[0], unescapeMount(f[1]), f[2]})
	}
	return out
}

func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func diskAlerts() []Alert {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Alert
	for _, m := range parseMounts(f) {
		var st syscall.Statfs_t
		if syscall.Statfs(m.point, &st) != nil || st.Blocks == 0 {
			continue
		}
		bs := uint64(st.Bsize)
		used := (st.Blocks - st.Bfree) * bs
		avail := st.Bavail * bs
		pct := 100 * float64(used) / float64(used+avail)
		if a, ok := diskAlert(m.point, pct, avail); ok {
			out = append(out, a)
		}
	}
	return out
}

func diskAlert(point string, pct float64, free uint64) (Alert, bool) {
	if pct <= 85 {
		return Alert{}, false
	}
	sev := "warn"
	if pct > 95 {
		sev = "err"
	}
	return Alert{
		ID: "disk:" + point, Severity: sev, TitleKey: "diskFull",
		Title: fmt.Sprintf("%s is %.0f%% full", point, pct), Vars: map[string]any{"mount": point, "percent": int(pct + 0.5)},
		Detail: fmt.Sprintf("%.1f GiB free", float64(free)/(1<<30)), Action: &Action{Section: "files", Params: map[string]string{"path": point}},
	}, true
}

func swapAlert() []Alert {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil
	}
	defer f.Close()
	var total, free uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fs := strings.Fields(v)
		if len(fs) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(fs[0], 10, 64)
		switch k {
		case "SwapTotal":
			total = n
		case "SwapFree":
			free = n
		}
	}
	return swapFrom(total, free)
}

func swapFrom(totalKB, freeKB uint64) []Alert {
	if totalKB == 0 || freeKB > totalKB {
		return nil
	}
	pct := 100 * float64(totalKB-freeKB) / float64(totalKB)
	if pct <= 50 {
		return nil
	}
	return []Alert{{ID: "swap", Severity: "warn", TitleKey: "swapHigh", Title: fmt.Sprintf("Swap is %.0f%% used", pct),
		Vars: map[string]any{"percent": int(pct + 0.5)}, Detail: "The machine may be short of memory", Action: &Action{Section: "terminal"}}}
}
