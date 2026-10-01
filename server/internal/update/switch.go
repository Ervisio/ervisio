package update

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
	"github.com/Fonlogen/LinuxAdmin/server/internal/config"
)

// Service restarts the daemon.
type Service interface {
	Restart(ctx context.Context) error
}

// Health waits until the daemon answers as version want ("" = any version
// answering, for old builds without /api/health).
type Health interface {
	Wait(ctx context.Context, want string, timeout time.Duration) error
}

// Prober runs `<binary> --version` and returns the printed version.
type Prober func(ctx context.Context, binary string) (string, error)

// HealthTimeout is how long a new version has to answer after a restart.
const HealthTimeout = 30 * time.Second

// Switcher moves `current` to another installed version, restarts the
// daemon and rolls back when the new version does not become healthy. It
// runs outside linuxadmin.service (a transient systemd unit), since
// restarting the service kills everything in it.
type Switcher struct {
	Layout  *Layout
	State   *State
	Service Service
	Health  Health
	Probe   Prober
	Timeout time.Duration
	Logf    func(format string, args ...any)
}

func (s *Switcher) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Switch activates version target. kind is KindUpdate or KindRollback.
// The result is also written to last.json.
func (s *Switcher) Switch(ctx context.Context, target, kind string, auto bool) Result {
	r := Result{State: StateRunning, Kind: kind, To: target, StartedAt: nowMs(), Auto: auto, PID: os.Getpid()}
	fail := func(state string, err error) Result {
		r.State, r.Error, r.FinishedAt, r.PID = state, err.Error(), nowMs(), 0
		s.logf("%s to %s: %s: %v", kind, target, state, err)
		if werr := s.State.WriteLast(r); werr != nil {
			s.logf("write last.json: %v", werr)
		}
		return r
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = HealthTimeout
	}

	old, err := s.Layout.Current()
	if err != nil {
		return fail(StateFailed, fmt.Errorf("no current version: %w", err))
	}
	r.From = old
	if old == target {
		return fail(StateFailed, fmt.Errorf("version %s is already active", target))
	}
	if !ValidDirName(target) || !isRegular(s.Layout.DaemonPath(target)) || !isRegular(s.Layout.BridgePath(target)) {
		return fail(StateFailed, fmt.Errorf("version %s is not installed", target))
	}
	want := s.probe(ctx, s.Layout.DaemonPath(target))
	wantOld := s.probe(ctx, s.Layout.DaemonPath(old))
	if err := s.State.WriteLast(r); err != nil {
		s.logf("write last.json: %v", err)
	}

	s.logf("%s: switching from %s to %s", kind, old, target)
	if err := s.Layout.SetCurrent(target); err != nil {
		return fail(StateFailed, fmt.Errorf("switch: %w", err))
	}
	err = s.Service.Restart(ctx)
	if err == nil {
		err = s.Health.Wait(ctx, want, timeout)
	}
	if err == nil {
		if perr := s.Layout.SetPrevious(old); perr != nil {
			s.logf("record previous version: %v", perr)
		}
		if removed := s.Layout.Prune(); len(removed) > 0 {
			s.logf("removed old versions: %s", strings.Join(removed, ", "))
		}
		r.State, r.FinishedAt, r.PID = StateOK, nowMs(), 0
		if werr := s.State.WriteLast(r); werr != nil {
			s.logf("write last.json: %v", werr)
		}
		s.logf("%s to %s done", kind, target)
		return r
	}

	// The new version is not healthy: go back.
	s.logf("%s to %s failed (%v), rolling back to %s", kind, target, err, old)
	cause := err
	if rerr := s.Layout.SetCurrent(old); rerr != nil {
		return fail(StateFailed, fmt.Errorf("%v; rollback to %s failed: %v", cause, old, rerr))
	}
	if rerr := s.Service.Restart(ctx); rerr != nil {
		return fail(StateRolledBack, fmt.Errorf("%v; restart of %s after rollback failed: %v", cause, old, rerr))
	}
	if herr := s.Health.Wait(ctx, wantOld, timeout); herr != nil {
		s.logf("after rollback, %s is not answering either: %v", old, herr)
		cause = fmt.Errorf("%v; after rollback %s does not answer: %v", cause, old, herr)
	}
	if kind == KindUpdate {
		s.Layout.Prune() // drops the broken version
	}
	return fail(StateRolledBack, cause)
}

func (s *Switcher) probe(ctx context.Context, bin string) string {
	if s.Probe == nil {
		return ""
	}
	v, err := s.Probe(ctx, bin)
	if err != nil {
		return "" // an old build without --version: accept any healthy answer
	}
	return v
}

// ProbeBinary runs `bin --version` (10 s timeout) and returns its output.
func ProbeBinary(ctx context.Context, bin string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %v", bin, err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" || len(v) > 128 || strings.ContainsAny(v, "\n\r") {
		return "", fmt.Errorf("%s --version printed an unexpected answer", bin)
	}
	return v, nil
}

// Systemctl restarts a unit with systemctl.
type Systemctl struct{ Unit string }

// Restart runs `systemctl restart <unit>`.
func (s Systemctl) Restart(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "restart", s.Unit)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart %s: %v %s", s.Unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HTTPHealth polls the daemon's /api/health on the loopback address of its
// listen setting.
type HTTPHealth struct {
	// Base is the URL to poll, e.g. https://127.0.0.1:9090.
	Base   string
	Client *http.Client
	Every  time.Duration
}

// NewHTTPHealth builds a checker from the daemon configuration file.
func NewHTTPHealth(configPath string) *HTTPHealth {
	cfg, _, _, err := config.Load(configPath)
	if err != nil || cfg == nil {
		cfg = config.Default()
	}
	return &HTTPHealth{
		Base: "https://" + LoopbackAddr(cfg.Listen),
		Client: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				// Loopback to our own daemon, whose certificate is usually
				// self-signed; the version in the answer is what matters.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				Proxy:           nil,
			},
		},
	}
}

// LoopbackAddr turns a listen address into one to connect to locally.
func LoopbackAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "127.0.0.1:9090"
	}
	ip := net.ParseIP(host)
	switch {
	case host == "" || (ip != nil && ip.Equal(net.IPv4zero)):
		host = "127.0.0.1"
	case ip != nil && ip.Equal(net.IPv6unspecified):
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}

// HealthInfo is the body of GET /api/health.
type HealthInfo struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	StartedAt int64  `json:"startedAt"`
}

// Wait implements Health.
func (h *HTTPHealth) Wait(ctx context.Context, want string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	every := h.Every
	if every <= 0 {
		every = time.Second
	}
	var last error = errors.New("no answer")
	for {
		if err := h.once(ctx, want); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("not healthy after %s: %v", timeout, last)
		case <-time.After(every):
		}
	}
}

func (h *HTTPHealth) once(ctx context.Context, want string) error {
	path := "/api/health"
	if want == "" {
		path = "/api/public/host" // old builds have no /api/health
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.Base+path, nil)
	if err != nil {
		return err
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return cleanNetErr(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", path, resp.Status)
	}
	if want == "" {
		return nil
	}
	var hi HealthInfo
	if err := json.Unmarshal(body, &hi); err != nil {
		return fmt.Errorf("unexpected health answer: %v", err)
	}
	if hi.Version != want {
		return fmt.Errorf("version %q answers, expected %q", hi.Version, want)
	}
	return nil
}

// HelperFlag starts the switch helper:
//
//	linuxadmind --apply-update <version> [--kind update|rollback] [--auto]
//
// It is started by updates.apply / updates.rollback (and the automatic
// install) in a transient systemd unit, never by hand.
const HelperFlag = "--apply-update"

// RunHelper is the entry point of HelperFlag. It returns the exit code.
func RunHelper(args []string) int {
	logf := func(format string, a ...any) {
		fmt.Fprintf(os.Stderr, brand.DaemonBinary+" update: "+format+"\n", a...)
	}
	if os.Geteuid() != 0 {
		logf("must run as root")
		return 2
	}
	var target, kind string = "", KindUpdate
	auto := false
	configPath := brand.ConfigPath
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--kind" && i+1 < len(args):
			kind = args[i+1]
			i++
		case a == "--config" && i+1 < len(args):
			configPath = args[i+1]
			i++
		case a == "--auto":
			auto = true
		case target == "" && !strings.HasPrefix(a, "-"):
			target = a
		default:
			logf("unexpected argument %q", a)
			return 2
		}
	}
	if !ValidDirName(target) || (kind != KindUpdate && kind != KindRollback) {
		logf("usage: %s %s <version> [--kind update|rollback]", brand.DaemonBinary, HelperFlag)
		return 2
	}
	st := DefaultState(brand.UpdatesDir)
	// updates.apply releases its lock just before starting us.
	var unlock func()
	var err error
	for i := 0; i < 30; i++ {
		if unlock, err = st.Lock(); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		logf("%v", err)
		return 1
	}
	defer unlock()
	sw := &Switcher{
		Layout:  DefaultLayout(),
		State:   st,
		Service: Systemctl{Unit: brand.ServiceUnit},
		Health:  NewHTTPHealth(configPath),
		Probe:   ProbeBinary,
		Logf:    logf,
	}
	res := sw.Switch(context.Background(), target, kind, auto)
	if res.State != StateOK {
		return 1
	}
	return 0
}
