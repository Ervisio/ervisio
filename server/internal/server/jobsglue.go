package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/jobs"
	"github.com/ervisio/ervisio/server/internal/modules/plugins"
	"github.com/ervisio/ervisio/server/internal/notify"
	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Background jobs, webhooks and notifications live in the daemon (not in
// a bridge): they must run while no page is open. This file connects the
// internal/jobs manager and the internal/notify service to the server:
// the executor that runs steps through the owner's bridge, the daemon-side
// RPC methods (plugins.jobs.*, plugins.notify, notify.*, jobs.*) and the
// unauthenticated webhook endpoint.

// initJobs creates the notification service and the job manager.
func (s *Server) initJobs() error {
	state := s.opts.StateDir
	if state == "" {
		state = brand.StateDir
	}
	nf := s.opts.NotifyFile
	if nf == "" {
		nf = filepath.Join(filepath.Dir(s.opts.ConfigPath), "notify.json")
	}
	s.jobPool = newBridgePool()
	s.notifier = notify.New(nf)
	s.notifier.Logf = s.log.Printf
	m, err := jobs.NewManager(jobs.Env{
		Dir:      filepath.Join(state, "jobs"),
		Manifest: plugins.Resolve,
		Account:  account.Lookup,
		OwnerOK:  s.jobOwnerOK,
		NewExecutor: func(a *account.Account) jobs.Executor {
			return &bridgeExecutor{s: s, a: a}
		},
		Notify: func(ctx context.Context, plugin string, msg notify.Message) error {
			return s.pluginNotify(ctx, plugin, msg)
		},
		Alert: func(msg notify.Message) {
			go s.notifier.Send(s.baseCtx, msg, notify.EventJobs)
		},
		Logf: s.log.Printf,
	})
	if err != nil {
		return err
	}
	s.jobs = m
	return nil
}

// NotifyCore sends a core notification (a new Ervisio release...) to the
// channels that subscribe to the event.
func (s *Server) NotifyCore(msg notify.Message, event string) {
	if s.notifier == nil {
		return
	}
	go s.notifier.Send(s.baseCtx, msg, event)
}

// jobOwnerOK says whether an account may still run jobs: the checks of a
// session's revalidation that do not need a session.
func (s *Server) jobOwnerOK(a *account.Account) string {
	cfg := s.Config()
	switch {
	case !account.ShellAllowed(a.Shell):
		return fmt.Sprintf("its login shell %q is no longer allowed", a.Shell)
	case a.IsRoot() && !cfg.AllowRoot:
		return "signing in as root is disabled (allow_root = false)"
	case !s.opts.NoAuth && !signInAllowed(cfg, a):
		return "it is no longer allowed to sign in (auth.allow_users, auth.allow_groups, auth.admins_only)"
	}
	if s.checker != nil {
		if sh, err := s.checker.shadow(a.Name); err == nil {
			if sh.Expired(time.Now()) {
				return "the account expired"
			}
		} else if err := s.checker.pamAcct(a.Name, ""); err != nil && errors.Is(err, pam.ErrAccount) {
			return fmt.Sprintf("PAM refused the account: %v", err)
		}
	}
	return ""
}

// bridgePool keeps the bridges that job runs use, so a job that runs every
// minute does not start a bridge (and open a PAM session) every minute: a
// bridge stays for jobBridgeIdle after its last run ends, then it is stopped.
type bridgePool struct {
	mu sync.Mutex
	m  map[string]*pooled
}

type pooled struct {
	p     *bridge.Proc
	refs  int
	timer *time.Timer
}

// jobBridgeIdle is how long an unused job bridge is kept.
const jobBridgeIdle = 5 * time.Minute

func newBridgePool() *bridgePool { return &bridgePool{m: map[string]*pooled{}} }

// acquire returns the live bridge for key, starting it with start when
// there is none, and counts a use. release must follow.
func (bp *bridgePool) acquire(key string, start func() (*bridge.Proc, error)) (*bridge.Proc, error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	e := bp.m[key]
	if e != nil && e.p.Alive() {
		e.refs++
		if e.timer != nil {
			e.timer.Stop()
			e.timer = nil
		}
		return e.p, nil
	}
	p, err := start()
	if err != nil {
		return nil, err
	}
	bp.m[key] = &pooled{p: p, refs: 1}
	return p, nil
}

func (bp *bridgePool) release(key string, p *bridge.Proc) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	e := bp.m[key]
	if e == nil || e.p != p {
		return
	}
	if e.refs--; e.refs > 0 {
		return
	}
	e.timer = time.AfterFunc(jobBridgeIdle, func() {
		bp.mu.Lock()
		cur := bp.m[key]
		stop := cur == e && e.refs == 0
		if stop {
			delete(bp.m, key)
		}
		bp.mu.Unlock()
		if stop {
			e.p.Stop()
		}
	})
}

func (bp *bridgePool) closeAll() {
	bp.mu.Lock()
	all := bp.m
	bp.m = map[string]*pooled{}
	bp.mu.Unlock()
	for _, e := range all {
		if e.timer != nil {
			e.timer.Stop()
		}
		e.p.Stop()
	}
}

// bridgeExecutor runs the steps of one run: the user's bridge for user
// steps, a root bridge (started directly: the daemon is root) for the
// steps of a job an administrator approved. Bridges come from the pool.
type bridgeExecutor struct {
	s *Server
	a *account.Account

	mu   sync.Mutex
	held map[string]*bridge.Proc
}

func (e *bridgeExecutor) proc(ctx context.Context, admin bool) (*bridge.Proc, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := "u:" + strconv.FormatUint(uint64(e.a.UID), 10)
	if admin {
		key = "root"
	}
	if p := e.held[key]; p != nil && p.Alive() {
		return p, nil
	}
	if e.held == nil {
		e.held = map[string]*bridge.Proc{}
	}
	p, err := e.s.jobPool.acquire(key, func() (*bridge.Proc, error) {
		if admin {
			ra, err := account.Lookup("root")
			if err != nil {
				return nil, rpc.Errorf(rpc.Unavailable, "Cannot look up root: %v", err)
			}
			return bridge.StartRoot(ctx, e.s.rootSpec(ra))
		}
		p, err := bridge.StartUser(ctx, e.s.spec(e.a, ""))
		if err != nil {
			return nil, rpc.Errorf(rpc.Unavailable, "Could not start a bridge for %s: %v", e.a.Name, err)
		}
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	if old := e.held[key]; old != nil {
		e.s.jobPool.release(key, old)
	}
	e.held[key] = p
	return p, nil
}

func (e *bridgeExecutor) call(ctx context.Context, admin bool, method string, params, out any) error {
	p, err := e.proc(ctx, admin)
	if err != nil {
		return err
	}
	release := p.Hold()
	defer release()
	raw, err := p.Call(ctx, method, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (e *bridgeExecutor) Exec(ctx context.Context, admin bool, p plugins.ExecParams) (*plugins.ExecResult, error) {
	var r plugins.ExecResult
	if err := e.call(ctx, admin, "plugins.exec", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (e *bridgeExecutor) HTTP(ctx context.Context, admin bool, p plugins.HTTPParams) (*plugins.HTTPResult, error) {
	var r plugins.HTTPResult
	if err := e.call(ctx, admin, "plugins.http", p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (e *bridgeExecutor) Close() {
	e.mu.Lock()
	held := e.held
	e.held = nil
	e.mu.Unlock()
	for key, p := range held {
		e.s.jobPool.release(key, p)
	}
}

// rootSpec is the bridge spec of a root bridge the daemon starts itself.
func (s *Server) rootSpec(ra *account.Account) *bridge.Spec {
	sp := s.spec(ra, "")
	sp.SwitchUser, sp.SessionHelper = false, ""
	return sp
}

// ---- who is calling ----

// isAdmin: the session has administrator rights now. Root always has them;
// others after unlocking. A --dev-insecure-noauth daemon is not root and
// has no sudo, so there its (insecure) sessions count as unlocked.
func (s *Server) isAdmin(sess *Session) bool {
	return sess.Account.IsRoot() || s.rootBridge(sess) != nil || (s.opts.Dev && s.opts.NoAuth)
}

func (s *Server) jobCaller(sess *Session) jobs.Caller {
	a := sess.Account
	g := map[string]bool{}
	for _, n := range a.GroupNames {
		g[n] = true
	}
	return jobs.Caller{Name: a.Name, UID: a.UID, Groups: g, Admin: s.isAdmin(sess), CanSudo: a.CanSudo(), IsRoot: a.IsRoot()}
}

// ---- RPC methods of the daemon ----

type localHandler func(ctx context.Context, s *Server, sess *Session, params json.RawMessage) (any, error)

type localMethod struct {
	admin bool
	run   localHandler
}

func bind(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return rpc.Errorf(rpc.Invalid, "invalid params: %v", err)
	}
	return nil
}

// pluginRef is the part of the params every plugin-scoped method has.
type pluginRef struct {
	Plugin string `json:"plugin"`
}

var localMethods = map[string]localMethod{}

func init() {
	// Plugin-scoped (the SDK: sdk.api.jobs, sdk.api.notify). The broker
	// fills in "plugin" from the frame's manifest; the daemon checks the
	// caller's rights and the manifest again.
	localMethods["plugins.jobs.create"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p jobs.CreateReq
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.Create(s.jobCaller(sess), p)
	}}
	localMethods["plugins.jobs.list"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			Plugin string `json:"plugin"`
			Job    string `json:"job"`
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		if p.Plugin == "" {
			return nil, rpc.Errorf(rpc.Invalid, "Give the plugin id.")
		}
		return map[string]any{"instances": s.jobs.List(s.jobCaller(sess), p.Plugin, p.Job)}, nil
	}}
	localMethods["plugins.jobs.get"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ Plugin, ID string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.Get(s.jobCaller(sess), need(p.Plugin), p.ID)
	}}
	localMethods["plugins.jobs.update"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p jobs.UpdateReq
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		p.Plugin = need(p.Plugin)
		return s.jobs.Update(s.jobCaller(sess), p)
	}}
	localMethods["plugins.jobs.delete"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ Plugin, ID string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return struct{}{}, s.jobs.Delete(s.jobCaller(sess), need(p.Plugin), p.ID)
	}}
	localMethods["plugins.jobs.runNow"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ Plugin, ID string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		id, err := s.jobs.RunNow(s.jobCaller(sess), need(p.Plugin), p.ID)
		return map[string]string{"run": id}, err
	}}
	localMethods["plugins.jobs.history"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			Plugin, ID string
			Limit      int
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		runs, err := s.jobs.Runs(s.jobCaller(sess), need(p.Plugin), p.ID, p.Limit)
		return map[string]any{"runs": runs}, err
	}}
	localMethods["plugins.jobs.webhooks.create"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ Plugin, ID, Label string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.CreateWebhook(s.jobCaller(sess), need(p.Plugin), p.ID, p.Label)
	}}
	localMethods["plugins.jobs.webhooks.regenerate"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			Plugin, ID string
			Webhook    string
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.RegenerateWebhook(s.jobCaller(sess), need(p.Plugin), p.ID, p.Webhook)
	}}
	localMethods["plugins.jobs.webhooks.revoke"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			Plugin, ID string
			Webhook    string
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return struct{}{}, s.jobs.RevokeWebhook(s.jobCaller(sess), need(p.Plugin), p.ID, p.Webhook)
	}}
	localMethods["plugins.notify"] = localMethod{run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			pluginRef
			notify.Message
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		man, err := plugins.Resolve(p.Plugin)
		if err != nil {
			return nil, err
		}
		c := s.jobCaller(sess)
		if !man.CanBeUsedBy(c.Groups, c.CanSudo) {
			return nil, rpc.Errorf(rpc.Forbidden, "%s is not available to your account.", man.Name)
		}
		if !man.Capabilities.Notify {
			return nil, rpc.Errorf(rpc.Forbidden, "%s does not declare capabilities.notify.", man.Name)
		}
		msg := p.Message
		if msg.Title == "" || len(msg.Title) > 1000 {
			return nil, rpc.Errorf(rpc.Invalid, "A notification needs a title.")
		}
		if msg.Level != "" && !notify.ValidLevel(msg.Level) {
			return nil, rpc.Errorf(rpc.Invalid, "The level must be info, success, warn or error.")
		}
		if !notify.ValidLink(msg.Link) {
			return nil, rpc.Errorf(rpc.Invalid, "The link must be an http(s) address or a path of this app.")
		}
		msg.Source = man.Name
		d, err := s.sendPlugin(ctx, p.Plugin, msg)
		if err != nil {
			return nil, err
		}
		return d, nil
	}}

	// Settings (administrators).
	localMethods["notify.list"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		l, err := s.notifier.List()
		if err != nil {
			return nil, rpc.Errorf(rpc.Internal, "Could not read the notification channels: %v", err)
		}
		if l == nil {
			l = []notify.Channel{}
		}
		return map[string]any{"channels": l, "file": s.notifier.Store.Path}, nil
	}}
	localMethods["notify.save"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var spec notify.Spec
		if err := bind(raw, &spec); err != nil {
			return nil, err
		}
		c, err := s.notifier.Save(spec)
		if err != nil {
			return nil, notifyErr(err)
		}
		return c, nil
	}}
	localMethods["notify.delete"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ ID string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return struct{}{}, notifyErr(s.notifier.Delete(p.ID))
	}}
	localMethods["notify.test"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var spec notify.Spec
		if err := bind(raw, &spec); err != nil {
			return nil, err
		}
		if err := s.notifier.Test(ctx, spec); err != nil {
			return nil, notifyErr(err)
		}
		return struct{}{}, nil
	}}
	localMethods["jobs.list"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p pluginRef
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return map[string]any{"instances": s.jobs.List(s.jobCaller(sess), p.Plugin, "")}, nil
	}}
	localMethods["jobs.setEnabled"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			ID      string
			Enabled bool
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.SetEnabled(s.jobCaller(sess), p.ID, p.Enabled)
	}}
	localMethods["jobs.runNow"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ ID string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		id, err := s.jobs.RunNow(s.jobCaller(sess), "", p.ID)
		return map[string]string{"run": id}, err
	}}
	localMethods["jobs.delete"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ ID string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return struct{}{}, s.jobs.Delete(s.jobCaller(sess), "", p.ID)
	}}
	localMethods["jobs.history"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			ID    string
			Limit int
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		runs, err := s.jobs.Runs(s.jobCaller(sess), "", p.ID, p.Limit)
		return map[string]any{"runs": runs}, err
	}}
	localMethods["jobs.webhooks.create"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct{ ID, Label string }
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.CreateWebhook(s.jobCaller(sess), "", p.ID, p.Label)
	}}
	localMethods["jobs.webhooks.regenerate"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			ID      string
			Webhook string
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return s.jobs.RegenerateWebhook(s.jobCaller(sess), "", p.ID, p.Webhook)
	}}
	localMethods["jobs.webhooks.revoke"] = localMethod{admin: true, run: func(ctx context.Context, s *Server, sess *Session, raw json.RawMessage) (any, error) {
		var p struct {
			ID      string
			Webhook string
		}
		if err := bind(raw, &p); err != nil {
			return nil, err
		}
		return struct{}{}, s.jobs.RevokeWebhook(s.jobCaller(sess), "", p.ID, p.Webhook)
	}}
}

// need returns the plugin id of a plugin-scoped call; "-" (matches no
// plugin) when the caller gave none, so the call is "not found".
func need(plugin string) string {
	if plugin == "" {
		return "-"
	}
	return plugin
}

// notifyErr turns an error of internal/notify into an RPC error whose
// message already carries no secret.
func notifyErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, notify.ErrNotFound):
		return rpc.Errorf(rpc.NotFound, "%v", err)
	}
	var re *rpc.Error
	if errors.As(err, &re) {
		return err
	}
	// Validation messages and delivery errors are written for people.
	return rpc.Errorf(rpc.Invalid, "%v", err)
}

// sendPlugin applies the plugin's rate limit and sends the message.
func (s *Server) sendPlugin(ctx context.Context, plugin string, msg notify.Message) (notify.Delivery, error) {
	if ok, wait := s.notifier.AllowPlugin(plugin); !ok {
		secs := int(wait.Seconds()) + 1
		return notify.Delivery{}, rpc.Errorf(rpc.Unavailable, "%s sent too many notifications. Try again in %d seconds.", plugin, secs).
			WithData(map[string]any{"reason": "rate_limited", "retryAfter": secs})
	}
	return s.notifier.Send(ctx, msg, notify.EventPlugins), nil
}

func (s *Server) pluginNotify(ctx context.Context, plugin string, msg notify.Message) error {
	_, err := s.sendPlugin(ctx, plugin, msg)
	return err
}

// handleLocalRPC serves the daemon's own methods. It reports whether the
// method was one of them.
func (s *Server) handleLocalRPC(w http.ResponseWriter, r *http.Request, sess *Session, req rpcRequest) bool {
	lm, ok := localMethods[req.Method]
	if !ok {
		return false
	}
	if s.jobs == nil || s.notifier == nil {
		writeError(w, rpc.Errorf(rpc.Unavailable, "background jobs are not available"))
		return true
	}
	if lm.admin && !s.isAdmin(sess) {
		writeError(w, rpc.Errorf(rpc.NeedsAdmin, "administrator rights are needed").WithData(map[string]string{"method": req.Method}))
		return true
	}
	if lm.admin {
		sess.touchAdmin()
	}
	res, err := lm.run(r.Context(), s, sess, req.Params)
	if err != nil {
		if r.Context().Err() != nil {
			return true
		}
		var re *rpc.Error
		if !errors.As(err, &re) {
			s.log.Printf("%s: %v", req.Method, err)
			re = rpc.Errorf(rpc.Internal, "internal error")
		}
		writeError(w, re)
		return true
	}
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{res})
	return true
}

// ---- webhooks ----

// handleHook serves POST /hooks/<plugin>/<token>: no session, no CSRF
// header (CI systems and registries call it). Whatever goes wrong, the
// answer says as little as possible.
func (s *Server) handleHook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.jobs == nil {
		hookReply(w, http.StatusNotFound, nil)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, jobs.MaxHookBody))
	res := s.jobs.HandleHook(r.PathValue("plugin"), r.PathValue("token"), limiterKey(s.realClientIP(r)), body, r.URL.Query())
	if res.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(res.RetryAfter))
	}
	switch res.Status {
	case http.StatusAccepted:
		hookReply(w, res.Status, map[string]string{"run": res.Run})
	case http.StatusBadRequest:
		hookReply(w, res.Status, map[string]string{"error": "invalid request"})
	case http.StatusTooManyRequests:
		hookReply(w, res.Status, map[string]string{"error": "too many requests"})
	default:
		hookReply(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func hookReply(w http.ResponseWriter, status int, v any) {
	if v == nil {
		v = map[string]string{"error": "not found"}
	}
	writeJSON(w, status, v)
}
