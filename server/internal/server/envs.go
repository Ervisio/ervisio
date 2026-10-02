package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/envs"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// envState is the daemon's side of environments (docs/api/environments.md).
type envState struct {
	m *envs.Manager

	mu      sync.Mutex
	remotes map[string]*remoteProc // envID:user -> bridge on the other server

	// pairConns are the live pairing connections of other servers on this one
	// (pairing id -> closers), so a revoked pairing is cut at once.
	pairConns map[string]map[int]func()
	pairSeq   int
}

type remoteProc struct {
	p    *bridge.Proc
	used time.Time
}

// newEnvState opens the environments store. A failure disables the feature
// (every envs call answers unavailable) but does not stop the daemon.
func (s *Server) newEnvState() *envState {
	dir, tun := s.opts.EnvsDir, s.opts.TunnelDir
	if dir == "" || tun == "" {
		if s.opts.Dev {
			_, port, _ := splitPort(s.ListenAddr())
			home, _ := os.UserHomeDir()
			if dir == "" {
				dir = filepath.Join(home, ".local", "state", "ervisio-dev", port, "envs")
			}
			if tun == "" {
				rt := os.Getenv("XDG_RUNTIME_DIR")
				if rt == "" {
					rt = os.TempDir()
				}
				tun = filepath.Join(rt, "ervisio-dev", port, "tunnels")
			}
		} else {
			if dir == "" {
				dir = filepath.Join(brand.StateDir, "envs")
			}
			if tun == "" {
				tun = filepath.Join(brand.RunDir, "tunnels")
			}
		}
	}
	m, err := envs.NewManager(envs.Options{Dir: dir, TunnelDir: tun, ServerName: sys.Hostname(), Log: s.log})
	if err != nil {
		s.log.Printf("environments disabled: %v", err)
		return &envState{remotes: map[string]*remoteProc{}, pairConns: map[string]map[int]func(){}}
	}
	return &envState{m: m, remotes: map[string]*remoteProc{}, pairConns: map[string]map[int]func(){}}
}

func splitPort(addr string) (host, port string, err error) {
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr, "9090", nil
	}
	return addr[:i], addr[i+1:], nil
}

// envLoop runs the health checks and closes idle remote bridges.
func (s *Server) envLoop(ctx context.Context) {
	if s.env.m == nil {
		return
	}
	go s.env.m.Run(ctx)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.env.mu.Lock()
			for k, r := range s.env.remotes {
				go r.p.Stop()
				delete(s.env.remotes, k)
			}
			s.env.mu.Unlock()
			return
		case <-t.C:
			s.env.mu.Lock()
			for k, r := range s.env.remotes {
				if !r.p.Alive() || (!r.p.Busy() && time.Since(r.used) > 5*time.Minute) {
					go r.p.Stop()
					delete(s.env.remotes, k)
				}
			}
			s.env.mu.Unlock()
		}
	}
}

func (s *Server) envsOff() *rpc.Error {
	return rpc.Errorf(rpc.Unavailable, "Environments are not available: the server could not open its environments store (see the daemon log).")
}

func envErr(err error) *rpc.Error {
	var e *envs.Error
	if errors.As(err, &e) {
		return rpc.Errorf(rpc.Invalid, "%s", e.Msg)
	}
	return rpc.Errorf(rpc.Internal, "%v", err)
}

// pairingView is a pairing without its credential hash.
type pairingView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	User      string    `json:"user"`
	CreatedBy string    `json:"createdBy"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"lastUsed"`
	LastVia   string    `json:"lastVia,omitempty"`
	LastAddr  string    `json:"lastAddr,omitempty"`
}

// envsCall handles the envs.* and plugins.envs.* methods in the daemon
// (they need the daemon's secrets and connections, not a bridge).
func (s *Server) envsCall(ctx context.Context, sess *Session, method string, params json.RawMessage) (any, *rpc.Error, bool) {
	if !strings.HasPrefix(method, "envs.") && method != "plugins.envs.list" {
		return nil, nil, false
	}
	if s.env == nil || s.env.m == nil {
		return nil, s.envsOff(), true
	}
	m := s.env.m
	bind := func(v any) *rpc.Error {
		p := params
		if len(p) == 0 || string(p) == "null" {
			p = json.RawMessage("{}")
		}
		if err := json.Unmarshal(p, v); err != nil {
			return rpc.Errorf(rpc.Invalid, "invalid params for %s: %v", method, err)
		}
		return nil
	}
	if method == "plugins.envs.list" {
		return m.Briefs(sess.Account.Name, sess.Account.GroupNames), nil, true
	}
	if !s.isAdmin(sess) {
		return nil, rpc.Errorf(rpc.NeedsAdmin, "administrator rights are needed").WithData(map[string]string{"method": method}), true
	}
	sess.touchAdmin()
	switch method {
	case "envs.list":
		return map[string]any{"envs": m.List(), "pairings": pairingViews(m.Pairings()), "openTokens": m.OpenTokens(), "server": sys.Hostname()}, nil, true
	case "envs.probe":
		var in envs.Input
		if e := bind(&in); e != nil {
			return nil, e, true
		}
		r, err := m.Probe(ctx, in)
		if err != nil {
			return nil, envErr(err), true
		}
		return r, nil, true
	case "envs.create":
		var in envs.Input
		if e := bind(&in); e != nil {
			return nil, e, true
		}
		v, err := m.Create(ctx, in, sess.Account.Name)
		if err != nil {
			return nil, envErr(err), true
		}
		s.log.Printf("environments: %s added %q (%s)", sess.Account.Name, v.Name, v.Kind)
		return v, nil, true
	case "envs.update":
		var p struct {
			ID string `json:"id"`
			envs.Input
		}
		if e := bind(&p); e != nil {
			return nil, e, true
		}
		v, err := m.Update(ctx, p.ID, p.Input)
		if err != nil {
			return nil, envErr(err), true
		}
		s.log.Printf("environments: %s changed %q", sess.Account.Name, v.Name)
		return v, nil, true
	case "envs.delete":
		var p struct {
			ID string `json:"id"`
		}
		if e := bind(&p); e != nil {
			return nil, e, true
		}
		if err := m.Delete(ctx, p.ID); err != nil {
			return nil, envErr(err), true
		}
		s.dropRemotes(p.ID)
		s.log.Printf("environments: %s removed %s", sess.Account.Name, p.ID)
		return map[string]string{"id": p.ID}, nil, true
	case "envs.test":
		var p struct {
			ID string `json:"id"`
		}
		if e := bind(&p); e != nil {
			return nil, e, true
		}
		st := m.Test(ctx, p.ID)
		if st == nil {
			return nil, rpc.Errorf(rpc.NotFound, "There is no such environment."), true
		}
		return st, nil, true
	case "envs.pairToken.create":
		var p struct {
			User string `json:"user"`
		}
		if e := bind(&p); e != nil {
			return nil, e, true
		}
		return s.createPairToken(sess, p.User)
	case "envs.pairings.revoke":
		var p struct {
			ID string `json:"id"`
		}
		if e := bind(&p); e != nil {
			return nil, e, true
		}
		ok, err := m.RevokePairing(p.ID)
		if err != nil {
			return nil, envErr(err), true
		}
		if !ok {
			return nil, rpc.Errorf(rpc.NotFound, "There is no such pairing."), true
		}
		s.closePairConns(p.ID)
		s.log.Printf("environments: %s revoked pairing %s", sess.Account.Name, p.ID)
		return map[string]string{"id": p.ID}, nil, true
	}
	return nil, rpc.Errorf(rpc.NotFound, "unknown method %s", method), true
}

func pairingViews(ps []envs.Pairing) []pairingView {
	out := make([]pairingView, 0, len(ps))
	for _, p := range ps {
		out = append(out, pairingView{p.ID, p.Name, p.User, p.CreatedBy, p.Created, p.LastUsed, p.LastVia, p.LastAddr})
	}
	return out
}

// pairUser resolves the local account a pairing runs as and applies the
// same rules as a sign-in: allowed by auth.allow_*, root only with allow_root.
func (s *Server) pairUser(name string) (*account.Account, *rpc.Error) {
	acc, err := account.Lookup(name)
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "There is no local user %q on this server.", name)
	}
	cfg := s.Config()
	if acc.IsRoot() && !cfg.AllowRoot {
		return nil, rpc.Errorf(rpc.Forbidden, "A pairing may not run as root (allow_root is off). Choose another user.")
	}
	if !signInAllowed(cfg, acc) {
		return nil, rpc.Errorf(rpc.Forbidden, "%s is not allowed to sign in here (auth.allow_users / auth.allow_groups), so a pairing cannot run as that user.", name)
	}
	if s.opts.Dev && s.devUser != nil && acc.UID != s.devUser.UID {
		return nil, rpc.Errorf(rpc.Forbidden, "In development mode only %s can be used.", s.devUser.Name)
	}
	return acc, nil
}

func (s *Server) createPairToken(sess *Session, user string) (any, *rpc.Error, bool) {
	if user == "" {
		user = sess.Account.Name
	}
	acc, e := s.pairUser(user)
	if e != nil {
		return nil, e, true
	}
	tok, exp, err := s.env.m.CreatePairToken(sess.Account.Name, acc.Name)
	if err != nil {
		return nil, envErr(err), true
	}
	s.log.Printf("environments: %s created a pairing token (runs as %s, expires %s)", sess.Account.Name, acc.Name, exp.Format(time.RFC3339))
	fp := ""
	if !s.opts.Dev && s.Config().TLS.Mode != "http" {
		if c, err := s.tlsConfig(); err == nil && len(c.Certificates) > 0 && len(c.Certificates[0].Certificate) > 0 {
			fp = envs.CertFingerprint(c.Certificates[0].Certificate[0])
		}
	}
	return map[string]any{"token": tok, "expires": exp, "user": acc.Name, "fingerprint": fp, "server": sys.Hostname(), "ttlMinutes": int(envs.TokenTTL.Minutes())}, nil, true
}

// envMethods are the calls that may target an environment ("env" param).
var envMethods = map[string]bool{
	"plugins.http": true, "plugins.httpStream": true,
	"plugins.exec": true, "plugins.execStream": true, "plugins.pty": true,
	"plugins.httpDownload": true, "plugins.httpUpload": true, "plugins.execDownload": true,
}

// routeWithEnv is route for the methods above: without "env" it is route;
// with one it checks the environment (access list, the plugin's opt-in) and
//   - for tcp-tls, ssh and portainer-agent hands the user's bridge a tunnel
//     socket in the "envSocket" param (the daemon removes any "envSocket" the
//     browser sent), or
//   - for another Ervisio server returns the bridge running there.
//
// release must be called when the call or stream ends.
func (s *Server) routeWithEnv(ctx context.Context, sess *Session, method string, params json.RawMessage, admin bool) (p *bridge.Proc, isAdmin bool, out json.RawMessage, release func(), e *rpc.Error) {
	release = func() {}
	if !envMethods[method] || len(params) == 0 {
		p, isAdmin, e = s.route(ctx, sess, method, admin)
		return p, isAdmin, params, release, e
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(params, &obj) != nil || obj == nil {
		p, isAdmin, e = s.route(ctx, sess, method, admin)
		return p, isAdmin, params, release, e
	}
	_, hadSock := obj["envSocket"]
	delete(obj, "envSocket")
	var envID string
	if raw, ok := obj["env"]; ok {
		delete(obj, "env")
		hadSock = true
		if json.Unmarshal(raw, &envID) != nil {
			return nil, false, nil, release, rpc.Errorf(rpc.Invalid, "env must be an environment id")
		}
	}
	rebuilt := params
	if hadSock {
		b, err := json.Marshal(obj)
		if err != nil {
			return nil, false, nil, release, rpc.Errorf(rpc.Invalid, "invalid params")
		}
		rebuilt = b
	}
	if envID == "" {
		p, isAdmin, e = s.route(ctx, sess, method, admin)
		return p, isAdmin, rebuilt, release, e
	}
	if s.env == nil || s.env.m == nil {
		return nil, false, nil, release, s.envsOff()
	}
	env := s.env.m.Get(envID)
	if env == nil {
		return nil, false, nil, release, rpc.Errorf(rpc.NotFound, "There is no environment %q.", envID)
	}
	if !env.Allowed(sess.Account.Name, sess.Account.GroupNames) {
		return nil, false, nil, release, rpc.Errorf(rpc.Forbidden, "Your account may not use the environment %q.", env.Name)
	}
	ub, e := s.userBridge(ctx, sess)
	if e != nil {
		return nil, false, nil, release, e
	}
	// The plugin's own bridge-side check: plugin usable by this user, and
	// the capability or command opts in with "remote" for this kind.
	chk := map[string]any{"method": method, "kind": env.Kind, "params": json.RawMessage(rebuilt)}
	if _, lerr := ub.Level("plugins.envCheck"); !lerr {
		return nil, false, nil, release, rpc.Errorf(rpc.Unavailable, "this server's plugin bridge does not support environments")
	}
	if _, err := ub.Call(ctx, "plugins.envCheck", chk); err != nil {
		return nil, false, nil, release, rpc.ToError(err, false)
	}
	if env.Kind == envs.KindErvisio {
		rp, e := s.remoteBridge(ctx, sess, env)
		if e != nil {
			return nil, false, nil, release, e
		}
		return rp, false, rebuilt, rp.Hold(), nil
	}
	path, err := s.env.m.Tunnel(env, int(sess.Account.UID), int(sess.Account.GID))
	if err != nil {
		return nil, false, nil, release, envErr(err)
	}
	obj["envSocket"], _ = json.Marshal(path)
	b, _ := json.Marshal(obj)
	return ub, false, b, release, nil
}

// remoteBridge returns the bridge of the paired server for this user.
func (s *Server) remoteBridge(ctx context.Context, sess *Session, env *envs.Env) (*bridge.Proc, *rpc.Error) {
	key := env.ID + ":" + sess.Account.Name
	s.env.mu.Lock()
	r := s.env.remotes[key]
	if r != nil && r.p.Alive() {
		r.used = time.Now()
		p := r.p
		s.env.mu.Unlock()
		return p, nil
	}
	s.env.mu.Unlock()
	conn, err := s.env.m.DialPair(ctx, env, sess.Account.Name)
	if err != nil {
		return nil, envErr(err)
	}
	p, err := bridge.NewRemote(ctx, conn, "remote "+env.Name)
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
	}
	s.env.mu.Lock()
	if old := s.env.remotes[key]; old != nil && old.p.Alive() {
		s.env.mu.Unlock()
		go p.Stop()
		return old.p, nil
	}
	s.env.remotes[key] = &remoteProc{p: p, used: time.Now()}
	s.env.mu.Unlock()
	return p, nil
}

func (s *Server) dropRemotes(envID string) {
	s.env.mu.Lock()
	defer s.env.mu.Unlock()
	for k, r := range s.env.remotes {
		if strings.HasPrefix(k, envID+":") {
			go r.p.Stop()
			delete(s.env.remotes, k)
		}
	}
}

// trackPairConn registers a live pairing connection; the returned function
// forgets it.
func (s *Server) trackPairConn(id string, closer func()) func() {
	s.env.mu.Lock()
	defer s.env.mu.Unlock()
	s.env.pairSeq++
	n := s.env.pairSeq
	if s.env.pairConns[id] == nil {
		s.env.pairConns[id] = map[int]func(){}
	}
	s.env.pairConns[id][n] = closer
	return func() {
		s.env.mu.Lock()
		delete(s.env.pairConns[id], n)
		s.env.mu.Unlock()
	}
}

// closePairConns cuts every live connection of a pairing (it was revoked).
func (s *Server) closePairConns(id string) {
	s.env.mu.Lock()
	cs := s.env.pairConns[id]
	delete(s.env.pairConns, id)
	s.env.mu.Unlock()
	for _, c := range cs {
		c()
	}
}
