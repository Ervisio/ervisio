package software

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// TxParams are the parameters of software.transaction.
type TxParams struct {
	Op       string   `json:"op"` // upgrade | install | remove
	Packages []string `json:"packages"`
	// Source routes the change: repo (the system package manager, default),
	// flatpak, aur (not supported) or all (upgrade everything, repo + flatpak).
	Source string `json:"source"`
	// Scope is the Flatpak installation: system (default) or user.
	Scope string `json:"scope"`
}

// txState is the public state of the running (or last finished) transaction.
// It is mirrored to a state file; for the root bridge a copy without the log
// is world-readable so the user bridge can show progress.
type txState struct {
	Running      bool     `json:"running"`
	PID          int      `json:"pid"`
	Op           string   `json:"op"`
	Source       string   `json:"source"`
	Packages     []string `json:"packages"`
	StartedAt    int64    `json:"startedAt"`
	FinishedAt   int64    `json:"finishedAt,omitempty"`
	Done         int      `json:"done"`
	Total        int      `json:"total"`
	Current      string   `json:"current"`
	Step         int      `json:"step"`
	Steps        int      `json:"steps"`
	StepTitle    string   `json:"stepTitle"`
	OK           bool     `json:"ok"`
	Message      string   `json:"message,omitempty"`
	Hint         string   `json:"hint,omitempty"`
	RebootNeeded bool     `json:"rebootNeeded"`
	Log          []string `json:"log"`
}

var (
	txMu      sync.Mutex
	txRunning bool

	// stateRoot is where the root bridge keeps the full transaction state
	// (0600: the log may show package names, paths and errors), and
	// stateRootPublic a summary without the log (0644) for the user bridges.
	stateRoot       = brand.RunDir + "/software-transaction.json"
	stateRootPublic = brand.RunDir + "/software-transaction.public.json"
	// stateRootOwner owns the root state files; readers ignore any other file.
	stateRootOwner = 0
)

// userStatePath is the state file of user-scope transactions. It lives only in
// $XDG_RUNTIME_DIR (a 0700 folder of the user): without one there is no user
// state file, never a predictable name in a shared folder.
func userStatePath() string {
	if isRoot() {
		return ""
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(dir) || checkTrustedDir(dir, os.Geteuid()) != nil {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("%s-software-%d.json", brand.Slug, os.Getuid()))
}

// statePaths returns where a transaction's state goes: the full file and, for
// the root bridge, the public summary.
func statePaths(admin bool) (full, public string) {
	if admin || isRoot() {
		return stateRoot, stateRootPublic
	}
	return userStatePath(), ""
}

const logKeep = 300

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// scanLines splits on \n and \r so progress bars that redraw a line are seen.
func scanLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type txRun struct {
	mu       sync.Mutex
	st       txState
	path     string // full state, readable by the owner only
	public   string // summary without the log, readable by everyone ("" = none)
	lastSave time.Time
	send     func(any) bool
}

func (r *txRun) save(force bool) {
	if r.path == "" {
		return
	}
	if !force && time.Since(r.lastSave) < 400*time.Millisecond {
		return
	}
	r.lastSave = time.Now()
	b, err := json.Marshal(&r.st)
	if err != nil {
		return
	}
	dir := filepath.Dir(r.path)
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		_ = os.Mkdir(dir, 0o755)
	}
	_ = writeFileAtomic(r.path, b, 0o600)
	if r.public != "" {
		sum := r.st
		sum.Log = []string{}
		if b, err := json.Marshal(&sum); err == nil {
			_ = writeFileAtomic(r.public, b, 0o644)
		}
	}
}

func (r *txRun) addLog(line string) {
	r.mu.Lock()
	r.st.Log = append(r.st.Log, line)
	if len(r.st.Log) > logKeep {
		r.st.Log = r.st.Log[len(r.st.Log)-logKeep:]
	}
	r.save(false)
	r.mu.Unlock()
	r.send(map[string]any{"type": "log", "line": line})
}

// hintFor recognises common failures in the output.
func hintFor(log []string) string {
	text := strings.ToLower(strings.Join(log, "\n"))
	switch {
	case strings.Contains(text, "unable to lock database"), strings.Contains(text, "could not get lock"), strings.Contains(text, "dpkg frontend lock"), strings.Contains(text, "another app is currently holding"):
		return "locked"
	case strings.Contains(text, "failed retrieving file"), strings.Contains(text, "404"), strings.Contains(text, "failed to fetch"):
		return "outdated"
	case strings.Contains(text, "conflicting files"), strings.Contains(text, "file conflicts"):
		return "conflict"
	case strings.Contains(text, "invalid or corrupted package"), strings.Contains(text, "signature"):
		return "signature"
	case strings.Contains(text, "could not resolve host"), strings.Contains(text, "failed to connect"), strings.Contains(text, "temporary failure in name resolution"):
		return "network"
	}
	return ""
}

func lockedByOther(m *manager) bool {
	if m.pac != nil {
		if _, err := os.Stat(filepath.Join(m.pac.db(), "db.lck")); err == nil {
			return true
		}
	}
	return false
}

// buildPlans turns the request into backend plans.
func (m *manager) buildPlans(p TxParams, admin bool) ([]Plan, error) {
	if err := validNames(p.Packages); err != nil {
		return nil, err
	}
	switch p.Op {
	case "upgrade", "install", "remove":
	default:
		return nil, rpc.Errorf(rpc.Invalid, "Unknown operation %q (use upgrade, install or remove).", p.Op)
	}
	if p.Scope != "" && p.Scope != "system" && p.Scope != "user" {
		return nil, rpc.Errorf(rpc.Invalid, "Unknown Flatpak scope %q.", p.Scope)
	}
	source := p.Source
	if source == "" {
		source = KindRepo
	}
	if !admin && !(source == KindFlatpak && p.Scope == "user") {
		return nil, rpc.Errorf(rpc.NeedsAdmin, "Changing system software needs administrator rights.")
	}
	if admin && source == KindFlatpak && p.Scope == "user" {
		return nil, rpc.Errorf(rpc.Invalid, "Flatpak apps in the user installation are managed without administrator rights.")
	}
	do := func(b Backend, pkgs []string) (Plan, error) {
		switch p.Op {
		case "install":
			return b.Install(pkgs)
		case "remove":
			return b.Remove(pkgs)
		}
		return b.Upgrade(pkgs)
	}
	var plans []Plan
	add := func(b Backend, pkgs []string) error {
		pl, err := do(b, pkgs)
		if err != nil {
			return err
		}
		plans = append(plans, pl)
		return nil
	}
	switch source {
	case "all":
		if p.Op != "upgrade" || len(p.Packages) > 0 {
			return nil, rpc.Errorf(rpc.Invalid, "Source \"all\" only works for upgrading everything.")
		}
		if m.primary != nil {
			if err := add(m.primary, nil); err != nil {
				return nil, err
			}
		}
		if m.flatpak != nil {
			if err := add(m.flatpak.(scoped).WithScope("system"), nil); err != nil {
				return nil, err
			}
		}
	case KindFlatpak:
		if m.flatpak == nil {
			return nil, rpc.Errorf(rpc.Unavailable, "Flatpak is not installed.")
		}
		scope := p.Scope
		if scope == "" {
			scope = "system"
		}
		if p.Op != "upgrade" && len(p.Packages) == 0 {
			return nil, orInvalid(nil)
		}
		if err := add(m.flatpak.(scoped).WithScope(scope), p.Packages); err != nil {
			return nil, err
		}
	case KindAUR:
		if p.Op == "remove" {
			if m.primary == nil {
				return nil, rpc.Errorf(rpc.Unavailable, "No supported system package manager was found.")
			}
			if err := add(m.primary, p.Packages); err != nil {
				return nil, err
			}
			break
		}
		return nil, errAURUnsupported
	default: // repo, or a repository name such as "core"
		if m.primary == nil {
			return nil, rpc.Errorf(rpc.Unavailable, "No supported system package manager was found.")
		}
		if p.Op != "upgrade" && len(p.Packages) == 0 {
			return nil, orInvalid(nil)
		}
		if err := add(m.primary, p.Packages); err != nil {
			return nil, err
		}
	}
	if len(plans) == 0 {
		return nil, rpc.Errorf(rpc.Unavailable, "Nothing to do: no package manager is available for this request.")
	}
	return plans, nil
}

// runTransaction executes a request and streams its progress. The command
// keeps running when the client goes away (killing a package manager half way
// would leave the system broken); the state file keeps the progress.
func runTransaction(ctx context.Context, c *rpc.Call, s rpc.Stream, user bool) error {
	var p TxParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	m := getManager()
	if err := m.requireAny(); err != nil {
		return err
	}
	plans, err := m.buildPlans(p, !user)
	if err != nil {
		return err
	}

	txMu.Lock()
	if txRunning {
		txMu.Unlock()
		return rpc.Errorf(rpc.Conflict, "Another software change is already running. Wait for it to finish.")
	}
	if lockedByOther(m) {
		txMu.Unlock()
		return rpc.Errorf(rpc.Conflict, "The package database is locked by another process (an update may already be running). Try again in a minute.")
	}
	txRunning = true
	txMu.Unlock()

	// Which pending updates need a reboot?
	reboot := false
	touchesSystem := p.Source != KindFlatpak
	if p.Op != "remove" && touchesSystem {
		if ups, _, _, err := m.updates(ctx, false); err == nil {
			sel := map[string]bool{}
			for _, n := range p.Packages {
				sel[n] = true
			}
			for _, u := range ups {
				if u.Kind == KindRepo && hasNote(u, "reboot") && (len(p.Packages) == 0 || sel[u.Name]) {
					reboot = true
				}
			}
		}
	}

	var sendMu sync.Mutex
	alive := true
	full, public := statePaths(c.Admin)
	run := &txRun{path: full, public: public}
	run.send = func(v any) bool {
		sendMu.Lock()
		defer sendMu.Unlock()
		if !alive {
			return false
		}
		if s.Send(v) != nil {
			alive = false
			return false
		}
		return true
	}
	steps := 0
	for _, pl := range plans {
		steps += len(pl.Steps)
	}
	src := p.Source
	if src == "" {
		src = KindRepo
	}
	run.st = txState{Running: true, PID: os.Getpid(), Op: p.Op, Source: src, Packages: append([]string{}, p.Packages...),
		StartedAt: time.Now().UnixMilli(), Steps: steps, Log: []string{}}
	run.save(true)
	run.send(map[string]any{"type": "start", "op": p.Op, "source": src, "packages": run.st.Packages, "steps": steps})

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ok, msg := executePlans(plans, run, p.Op)
		m.invalidate()
		run.mu.Lock()
		run.st.Running, run.st.OK, run.st.Message = false, ok, msg
		run.st.FinishedAt = time.Now().UnixMilli()
		if ok {
			run.st.Done = run.st.Total
			run.st.RebootNeeded = reboot || (touchesSystem && p.Op == "upgrade" && rebootPending())
		} else {
			run.st.Hint = hintFor(run.st.Log)
		}
		final := run.st
		run.save(true)
		run.mu.Unlock()
		txMu.Lock()
		txRunning = false
		txMu.Unlock()
		run.send(map[string]any{"type": "done", "ok": ok, "message": msg, "hint": final.Hint, "rebootNeeded": final.RebootNeeded})
	}()

	select {
	case <-finished:
	case <-ctx.Done():
		sendMu.Lock()
		alive = false
		sendMu.Unlock()
	}
	return nil
}

// executePlans runs every step in order and stops at the first failure.
func executePlans(plans []Plan, r *txRun, op string) (bool, string) {
	idx := 0
	ctx := context.Background()
	for _, pl := range plans {
		for _, st := range pl.Steps {
			idx++
			r.mu.Lock()
			r.st.Step, r.st.StepTitle = idx, st.Title
			r.st.Done, r.st.Total, r.st.Current = 0, 0, ""
			r.save(true)
			r.mu.Unlock()
			r.send(map[string]any{"type": "step", "index": idx, "title": st.Title, "command": st.Name + " " + strings.Join(st.Args, " ")})
			if err := runStep(ctx, st, r); err != nil {
				return false, err.Error()
			}
		}
	}
	r.mu.Lock()
	r.save(true)
	r.mu.Unlock()
	return true, ""
}

func runStep(ctx context.Context, st Step, r *txRun) error {
	cmd, err := sys.Cmd{Name: st.Name, Args: st.Args, Env: st.Env, Timeout: -1}.Command(ctx)
	if err != nil {
		return err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	r.addLog("$ " + st.Name + " " + strings.Join(st.Args, " "))
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return fmt.Errorf("could not start %s: %v", st.Name, err)
	}
	pw.Close()
	var prog Progress
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	sc.Split(scanLines)
	var tail []string
	for sc.Scan() {
		line := strings.TrimRight(ansiRe.ReplaceAllString(sc.Text(), ""), " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		r.addLog(line)
		tail = append(tail, line)
		if len(tail) > 6 {
			tail = tail[1:]
		}
		if st.Parse != nil && st.Parse(line, &prog) {
			r.mu.Lock()
			r.st.Done, r.st.Total, r.st.Current = prog.Done, prog.Total, prog.Current
			r.save(false)
			r.mu.Unlock()
			r.send(map[string]any{"type": "progress", "done": prog.Done, "total": prog.Total, "current": prog.Current})
		}
	}
	pr.Close()
	werr := cmd.Wait()
	if werr != nil {
		msg := strings.TrimSpace(strings.Join(lastErrors(tail), " "))
		if msg == "" {
			msg = fmt.Sprintf("%s exited with an error (%v)", st.Name, werr)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// lastErrors prefers lines that look like errors from the tail of the output.
func lastErrors(tail []string) []string {
	var errs []string
	for _, l := range tail {
		low := strings.ToLower(l)
		if strings.HasPrefix(low, "error") || strings.Contains(low, "error:") || strings.HasPrefix(low, "e:") || strings.Contains(low, "failed") {
			errs = append(errs, l)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	if len(tail) > 2 {
		return tail[len(tail)-2:]
	}
	return tail
}

// readStatus returns the transaction currently running (or finished last),
// reading the files of both bridges. The root bridge reads its full file; a
// user bridge reads the root's summary (no log) and its own file. A file is
// trusted only when it is a regular file of the expected owner.
func readStatus() (busy bool, tx *txState) {
	type src struct {
		path  string
		owner int
	}
	var srcs []src
	if isRoot() {
		srcs = []src{{stateRoot, stateRootOwner}}
	} else {
		srcs = []src{{stateRootPublic, stateRootOwner}}
		if p := userStatePath(); p != "" {
			srcs = append(srcs, src{p, os.Geteuid()})
		}
	}
	var best *txState
	for _, sf := range srcs {
		b, err := readOwnedFile(sf.path, sf.owner)
		if err != nil {
			continue
		}
		var st txState
		if json.Unmarshal(b, &st) != nil {
			continue
		}
		if st.Log == nil {
			st.Log = []string{}
		}
		if st.Running {
			if _, err := os.Stat(fmt.Sprintf("/proc/%d", st.PID)); err != nil || st.PID <= 0 {
				st.Running, st.OK, st.Message = false, false, "interrupted"
			}
		}
		if best == nil || (st.Running && !best.Running) || (st.Running == best.Running && st.StartedAt > best.StartedAt) {
			cp := st
			best = &cp
		}
	}
	if best != nil && !best.Running && best.FinishedAt > 0 && time.Since(time.UnixMilli(best.FinishedAt)) > time.Hour {
		best = nil
	}
	if best != nil && best.Running {
		busy = true
	}
	return busy, best
}
