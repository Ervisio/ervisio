//go:build unix

// Systemd backend (systemctl / journalctl).

package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

const showProps = "Id,Description,MemoryCurrent,CPUUsageNSec,MainPID,StateChangeTimestamp,UnitFileState,NextElapseUSecRealtime,LastTriggerUSec,Triggers,Listen"

// ---- systemctl helpers ----

func systemctl(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"--no-pager"}, args...)
	out, err := sys.Output(ctx, "systemctl", full...)
	if err != nil {
		return out, systemctlError(err)
	}
	return out, nil
}

// systemctlError turns a failed systemctl into a message for people.
func systemctlError(err error) error {
	var ee *sys.ExitError
	if errors.As(err, &ee) {
		msg := strings.TrimSpace(ee.Stderr)
		low := strings.ToLower(msg)
		switch {
		case strings.Contains(low, "system has not been booted with systemd"), strings.Contains(low, "failed to connect to bus"):
			return rpc.Errorf(rpc.Unavailable, "systemd is not running on this machine, so services cannot be managed.")
		case strings.Contains(low, "not found"), strings.Contains(low, "does not exist"), strings.Contains(low, "no such file"):
			return rpc.Errorf(rpc.NotFound, "%s", firstLine(msg))
		case strings.Contains(low, "access denied"), strings.Contains(low, "interactive authentication required"), strings.Contains(low, "permission denied"):
			return rpc.Errorf(rpc.NeedsAdmin, "Changing this service needs administrator rights.")
		case strings.Contains(low, "masked"):
			return rpc.Errorf(rpc.Conflict, "%s", firstLine(msg))
		case msg != "":
			return rpc.Errorf(rpc.Conflict, "%s", firstLine(msg))
		}
	}
	return err
}

func listUnitsRaw(ctx context.Context, typ string) ([]listedUnit, error) {
	out, err := systemctl(ctx, "list-units", "--type="+typ, "--all", "--output=json")
	if err != nil {
		return nil, err
	}
	return parseListUnits(out)
}

// baseUnits returns every unit of a type (loaded ones plus installed unit
// files that are not loaded), without CPU/memory details.
func baseUnits(ctx context.Context, typ string, fresh bool) ([]*Unit, error) {
	listed, err := listUnitsRaw(ctx, typ)
	if err != nil {
		return nil, err
	}
	files := unitFiles(ctx, typ, fresh)
	seen := map[string]bool{}
	var units []*Unit
	for _, l := range listed {
		if l.Load == "not-found" && l.Active != "failed" {
			continue
		}
		seen[l.Name] = true
		u := &Unit{Name: l.Name, Description: l.Description, Load: l.Load, Active: l.Active, Sub: l.Sub, Enabled: files[l.Name]}
		u.State = stateOf(u.Active, u.Sub)
		u.Purpose = PurposeOf(u.Name)
		units = append(units, u)
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := files[n]
		if seen[n] || strings.Contains(n, "@.") || st == "alias" {
			continue
		}
		u := &Unit{Name: n, Load: "not-loaded", Active: "inactive", Sub: "dead", State: StateStopped, Enabled: st, Purpose: PurposeOf(n)}
		units = append(units, u)
	}
	sort.Slice(units, func(i, j int) bool { return strings.ToLower(units[i].Name) < strings.ToLower(units[j].Name) })
	return units, nil
}

// unitFiles returns name -> enable state of installed unit files. `systemctl
// list-unit-files` is slow (0.3 s), so results are cached for a few seconds.
var (
	ufMu    sync.Mutex
	ufCache = map[string]struct {
		at    time.Time
		files map[string]string
	}{}
)

func unitFiles(ctx context.Context, typ string, fresh bool) map[string]string {
	ufMu.Lock()
	defer ufMu.Unlock()
	if c, ok := ufCache[typ]; ok && !fresh && time.Since(c.at) < 15*time.Second {
		return c.files
	}
	out, err := systemctl(ctx, "list-unit-files", "--type="+typ, "--output=json")
	if err != nil {
		return ufCache[typ].files
	}
	files, _ := parseUnitFiles(out)
	ufCache[typ] = struct {
		at    time.Time
		files map[string]string
	}{time.Now(), files}
	return files
}

// showMany runs `systemctl show` for several units, in chunks.
func showMany(ctx context.Context, props string, names []string) (map[string]map[string]string, error) {
	res := map[string]map[string]string{}
	for len(names) > 0 {
		n := len(names)
		if n > 150 {
			n = 150
		}
		args := []string{"show", "--timestamp=unix", "-p", props, "--"}
		args = append(args, names[:n]...)
		names = names[n:]
		out, err := systemctl(ctx, args...)
		if err != nil {
			return nil, err
		}
		for _, b := range parseShow(out) {
			if id := b["Id"]; id != "" {
				res[id] = b
			}
		}
	}
	return res, nil
}

// applyShow fills the details of u from a `systemctl show` property block.
func applyShow(u *Unit, p map[string]string) {
	if p == nil {
		return
	}
	if u.Description == "" {
		u.Description = p["Description"]
	}
	u.Memory = parseUint(p["MemoryCurrent"])
	u.CPUNs = parseUint(p["CPUUsageNSec"])
	if v := parseUint(p["MainPID"]); v != nil {
		u.PID = int(*v)
	}
	u.Since = parseUnixStamp(p["StateChangeTimestamp"])
	if u.Enabled == "" {
		u.Enabled = p["UnitFileState"]
	}
	if unitType(u.Name) == "timer" {
		u.Next = parseUSecStamp(p["NextElapseUSecRealtime"])
		u.Last = parseUSecStamp(p["LastTriggerUSec"])
	}
	if t := unitType(u.Name); t == "timer" || t == "socket" {
		u.Triggers = strings.TrimSpace(p["Triggers"])
	}
	if unitType(u.Name) == "socket" {
		for _, l := range strings.Split(p["Listen"], "\n") {
			if l = strings.TrimSpace(l); l != "" {
				u.Listen = append(u.Listen, l)
			}
		}
	}
}

// descCache remembers descriptions of installed-but-not-loaded units, which
// are costly to look up (systemd has to load each unit file).
var (
	descMu    sync.Mutex
	descCache = map[string]string{}
)

func detailUnits(ctx context.Context, units []*Unit) {
	var names []string
	descMu.Lock()
	for _, u := range units {
		if u.Load == "not-loaded" {
			if d, ok := descCache[u.Name]; ok {
				u.Description = d
				continue
			}
			names = append(names, u.Name)
		} else if u.Active != "inactive" {
			names = append(names, u.Name)
		}
	}
	descMu.Unlock()
	props, err := showMany(ctx, showProps, names)
	if err != nil {
		return // details are optional; the list is still useful
	}
	descMu.Lock()
	defer descMu.Unlock()
	for _, u := range units {
		if _, cached := descCache[u.Name]; cached && u.Load == "not-loaded" {
			continue
		}
		applyShow(u, props[u.Name])
		if u.Load == "not-loaded" {
			descCache[u.Name] = u.Description
		}
	}
}

// ---- services.list ----

func handleList(ctx context.Context, c *rpc.Call) (any, error) {
	var p listParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	typ, err := unitTypeParam(p.Type)
	if err != nil {
		return nil, err
	}
	units, err := baseUnits(ctx, typ, p.Fresh)
	if err != nil {
		return nil, err
	}
	detailUnits(ctx, units)
	return map[string]any{"units": units}, nil
}

// ---- services.summary ----

func handleSummary(ctx context.Context, c *rpc.Call) (any, error) {
	svc, err := baseUnits(ctx, "service", false)
	if err != nil {
		return nil, err
	}
	total, running, stopped := 0, 0, 0
	active := map[string]bool{}
	var failedNames []string
	desc := map[string]string{}
	for _, u := range svc {
		total++
		if u.Active == "active" {
			active[u.Name] = true
		}
		switch u.State {
		case StateRunning, StateFinished:
			running++
		case StateStopped:
			stopped++
		case StateFailed:
			failedNames = append(failedNames, u.Name)
			desc[u.Name] = u.Description
		}
	}
	timers, sockets := 0, 0
	if t, err := baseUnits(ctx, "timer", false); err == nil {
		timers = len(t)
	}
	if s, err := baseUnits(ctx, "socket", false); err == nil {
		sockets = len(s)
	}
	// Timers and sockets can fail too; they count as failed units.
	for _, typ := range []string{"timer", "socket"} {
		if l, err := listUnitsRaw(ctx, typ); err == nil {
			for _, u := range l {
				if u.Active == "failed" {
					failedNames = append(failedNames, u.Name)
					desc[u.Name] = u.Description
				}
			}
		}
	}
	failed := []FailedUnit{}
	if len(failedNames) > 0 {
		props, _ := showMany(ctx, "Id,Result,ExecMainStatus,StateChangeTimestamp", failedNames)
		for _, n := range failedNames {
			pr := props[n]
			code, _ := strconv.Atoi(pr["ExecMainStatus"])
			f := FailedUnit{Name: n, Description: desc[n], Since: parseUnixStamp(pr["StateChangeTimestamp"]), Result: pr["Result"], ExitCode: code}
			if h := hintFor(hintInput{Name: n, Result: f.Result, ExitCode: code, Active: active}); h != nil {
				f.Hint, f.HintID = h.Text, h.ID
			}
			failed = append(failed, f)
		}
	}
	return map[string]any{
		"total": total, "running": running, "stopped": stopped,
		"timers": timers, "sockets": sockets, "failed": failed,
	}, nil
}

// ---- services.get ----

func showOne(ctx context.Context, name string) (map[string]string, error) {
	out, err := systemctl(ctx, "show", "--timestamp=unix", "--", name)
	if err != nil {
		return nil, err
	}
	blocks := parseShow(out)
	if len(blocks) == 0 {
		return nil, rpc.Errorf(rpc.NotFound, "Unit %s was not found.", name)
	}
	p := blocks[0]
	if p["LoadState"] == "not-found" {
		return nil, rpc.Errorf(rpc.NotFound, "Unit %s was not found. It may have been removed or not installed.", name)
	}
	return p, nil
}

func handleGet(ctx context.Context, c *rpc.Call) (any, error) {
	var p nameParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkName(p.Name); err != nil {
		return nil, err
	}
	props, err := showOne(ctx, p.Name)
	if err != nil {
		return nil, err
	}
	u := &Unit{
		Name: p.Name, Description: props["Description"], Load: props["LoadState"],
		Active: props["ActiveState"], Sub: props["SubState"], Purpose: PurposeOf(p.Name),
	}
	u.State = stateOf(u.Active, u.Sub)
	applyShow(u, props)
	code, _ := strconv.Atoi(props["ExecMainStatus"])
	deps := map[string][]string{}
	for _, d := range depKeys {
		deps[d.key] = fields(props[d.prop])
	}
	return map[string]any{
		"unit":          u,
		"name":          u.Name,
		"description":   u.Description,
		"load":          u.Load,
		"active":        u.Active,
		"sub":           u.Sub,
		"state":         u.State,
		"enabled":       u.Enabled,
		"memory":        u.Memory,
		"cpuNs":         u.CPUNs,
		"pid":           u.PID,
		"since":         u.Since,
		"purpose":       u.Purpose,
		"type":          props["Type"],
		"path":          props["FragmentPath"],
		"dropIns":       fields(props["DropInPaths"]),
		"result":        props["Result"],
		"exitCode":      code,
		"documentation": fields(strings.ReplaceAll(props["Documentation"], `"`, "")),
		"canStart":      props["CanStart"] == "yes",
		"canStop":       props["CanStop"] == "yes",
		"canReload":     props["CanReload"] == "yes",
		"dependencies":  deps,
		"properties":    props,
	}, nil
}

// ---- services.action ----

func handleAction(ctx context.Context, c *rpc.Call) (any, error) {
	var p actionParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkName(p.Name); err != nil {
		return nil, err
	}
	switch p.Action {
	case "start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask":
	default:
		return nil, rpc.Errorf(rpc.Invalid, "Unknown action %q. Use start, stop, restart, reload, enable, disable, mask or unmask.", p.Action)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if _, err := systemctl(ctx, p.Action, "--", p.Name); err != nil {
		if ctx.Err() != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "systemd did not finish %s %s in time. Check the log for what it is waiting for.", p.Action, p.Name)
		}
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// ---- unit file and overrides ----

// overrideRoot is where administrator overrides live. Variable for tests.
var overrideRoot = "/etc/systemd/system"

const maxOverrideSize = 256 << 10

type fileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > 1<<20 {
		b = b[:1<<20]
	}
	return string(b)
}

func overridePath(name string) string {
	return filepath.Join(overrideRoot, name+".d", "override.conf")
}

func handleUnitFile(ctx context.Context, c *rpc.Call) (any, error) {
	var p nameParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkName(p.Name); err != nil {
		return nil, err
	}
	props, err := showOne(ctx, p.Name)
	if err != nil {
		return nil, err
	}
	path := props["FragmentPath"]
	res := map[string]any{"path": path, "content": "", "overrides": []fileEntry{}}
	if path != "" {
		res["content"] = readText(path)
	}
	ovs := []fileEntry{}
	hasOv := false
	for _, d := range fields(props["DropInPaths"]) {
		ovs = append(ovs, fileEntry{Path: d, Content: readText(d)})
		if d == overridePath(p.Name) {
			hasOv = true
		}
	}
	res["overrides"] = ovs
	res["overridePath"] = overridePath(p.Name)
	res["override"] = ""
	if hasOv {
		for _, o := range ovs {
			if o.Path == overridePath(p.Name) {
				res["override"] = o.Content
			}
		}
	} else if b, err := os.ReadFile(overridePath(p.Name)); err == nil {
		res["override"] = string(b)
	}
	return res, nil
}

func handleSaveOverride(ctx context.Context, c *rpc.Call) (any, error) {
	var p saveParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkName(p.Name); err != nil {
		return nil, err
	}
	if len(p.Content) > maxOverrideSize {
		return nil, rpc.Errorf(rpc.Invalid, "The override is too large (limit 256 KiB).")
	}
	if strings.ContainsRune(p.Content, 0) {
		return nil, rpc.Errorf(rpc.Invalid, "The override contains a NUL character. Remove it and try again.")
	}
	file := overridePath(p.Name)
	dir := filepath.Dir(file)
	if strings.TrimSpace(p.Content) == "" {
		// An empty override means "remove my changes".
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		_ = os.Remove(dir) // only succeeds when empty
	} else {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		content := p.Content
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if err := writeFileAtomic(file, []byte(content), 0o644); err != nil {
			return nil, err
		}
	}
	if _, err := systemctl(ctx, "daemon-reload"); err != nil {
		return nil, fmt.Errorf("the override was saved, but systemd could not reload its configuration: %w", err)
	}
	return map[string]any{"ok": true, "path": file}, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".override-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// ---- services.logs ----

func handleLogs(ctx context.Context, c *rpc.Call) (any, error) {
	var p logsParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if err := checkName(p.Name); err != nil {
		return nil, err
	}
	if p.Lines <= 0 {
		p.Lines = 100
	}
	if p.Lines > 1000 {
		p.Lines = 1000
	}
	out, err := sys.Output(ctx, "journalctl", "-u", p.Name, "-n", strconv.Itoa(p.Lines), "--no-pager", "-o", "json")
	if err != nil {
		var ee *sys.ExitError
		if errors.As(err, &ee) {
			return nil, rpc.Errorf(rpc.Unavailable, "The journal could not be read: %s", firstLine(ee.Stderr))
		}
		return nil, err
	}
	return map[string]any{"lines": parseJournal(out)}, nil
}

// ---- services.watch ----

func handleWatch(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p listParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	typ, err := unitTypeParam(p.Type)
	if err != nil {
		return err
	}
	prev, err := snapshot(ctx, typ)
	if err != nil {
		return err
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.Input():
		case <-tick.C:
		}
		cur, err := snapshot(ctx, typ)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue // transient failure; try again on the next tick
		}
		changed, removed := diffSnapshots(prev, cur)
		prev = cur
		if len(changed) == 0 && len(removed) == 0 {
			continue
		}
		units := make([]*Unit, 0, len(changed))
		for _, n := range changed {
			l := cur[n]
			u := &Unit{Name: n, Description: l.Description, Load: l.Load, Active: l.Active, Sub: l.Sub, Purpose: PurposeOf(n)}
			u.State = stateOf(u.Active, u.Sub)
			units = append(units, u)
		}
		var names []string
		for _, u := range units {
			names = append(names, u.Name)
		}
		if props, err := showMany(ctx, showProps, names); err == nil {
			for _, u := range units {
				applyShow(u, props[u.Name])
			}
		}
		if removed == nil {
			removed = []string{}
		}
		if err := s.Send(map[string]any{"units": units, "removed": removed}); err != nil {
			return nil
		}
	}
}

func snapshot(ctx context.Context, typ string) (map[string]listedUnit, error) {
	l, err := listUnitsRaw(ctx, typ)
	if err != nil {
		return nil, err
	}
	m := make(map[string]listedUnit, len(l))
	for _, u := range l {
		if u.Load == "not-found" && u.Active != "failed" {
			continue
		}
		m[u.Name] = u
	}
	return m, nil
}

// diffSnapshots returns the names that appeared or changed state, and the
// names that are gone from the loaded set.
func diffSnapshots(prev, cur map[string]listedUnit) (changed, removed []string) {
	for n, u := range cur {
		if o, ok := prev[n]; !ok || o.Active != u.Active || o.Sub != u.Sub || o.Load != u.Load || o.Description != u.Description {
			changed = append(changed, n)
		}
	}
	for n := range prev {
		if _, ok := cur[n]; !ok {
			removed = append(removed, n)
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)
	return
}
