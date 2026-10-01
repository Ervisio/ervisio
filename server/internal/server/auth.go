package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/brand"
	"github.com/Fonlogen/LinuxAdmin/server/internal/bridge"
	"github.com/Fonlogen/LinuxAdmin/server/internal/pam"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

var (
	errNotFound        = rpc.Errorf(rpc.NotFound, "not found")
	errUnauthenticated = rpc.Errorf(rpc.Unauthenticated, "not signed in")
	errBadCredentials  = rpc.Errorf(rpc.Unauthenticated, "wrong user name or password")
)

// csrf rejects state-changing requests that lack the X-Requested-With
// header or come from another origin. Browsers cannot add the custom header
// cross-origin without a CORS preflight, which we never approve.
func (s *Server) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if e := s.checkCSRF(r); e != nil {
			writeError(w, e)
			return
		}
		next(w, r)
	}
}

// csrfS is csrf for session handlers (checked after authentication, so a
// missing session is reported as unauthenticated first).
func (s *Server) csrfS(next sessionHandler) sessionHandler {
	return func(w http.ResponseWriter, r *http.Request, sess *Session) {
		if e := s.checkCSRF(r); e != nil {
			writeError(w, e)
			return
		}
		next(w, r, sess)
	}
}

func (s *Server) checkCSRF(r *http.Request) *rpc.Error {
	if r.Header.Get(brand.CSRFHeader) != brand.CSRFValue {
		return rpc.Errorf(rpc.Forbidden, "missing %s: %s header", brand.CSRFHeader, brand.CSRFValue)
	}
	if o := r.Header.Get("Origin"); o != "" && !s.originAllowed(o, r.Host) {
		return rpc.Errorf(rpc.Forbidden, "cross-origin request refused")
	}
	return nil
}

// originAllowed accepts the request's own host and, in dev mode, any
// loopback origin (the Vite dev server).
func (s *Server) originAllowed(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, host) {
		return true
	}
	if s.opts.Dev {
		h := u.Hostname()
		if h == "localhost" {
			return true
		}
		if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	return false
}

type sessionHandler func(w http.ResponseWriter, r *http.Request, sess *Session)

// authed resolves the session cookie; requests without a valid session get
// 401 {"error":{"code":"unauthenticated"}}.
func (s *Server) authed(next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.sessionFor(r)
		if sess == nil {
			writeError(w, errUnauthenticated)
			return
		}
		sess.touch(time.Now())
		next(w, r, sess)
	}
}

func (s *Server) sessionFor(r *http.Request) *Session {
	if c, err := r.Cookie(brand.SessionCookie); err == nil {
		if sess := s.sessions.get(c.Value); sess != nil {
			sess.mu.Lock()
			idle := time.Since(sess.lastSeen)
			sess.mu.Unlock()
			if idle <= s.Config().Session.Timeout.Duration {
				return sess
			}
			s.sessions.remove(sess)
		}
	}
	if s.opts.NoAuth {
		sess, err := s.noAuthSession()
		if err != nil {
			s.log.Printf("noauth session: %v", err)
			return nil
		}
		return sess
	}
	return nil
}

// noAuthSession returns (creating if needed) the dev session used by
// --dev-insecure-noauth.
func (s *Server) noAuthSession() (*Session, error) {
	s.noAuthMu.Lock()
	defer s.noAuthMu.Unlock()
	if s.noAuthSess != nil {
		select {
		case <-s.noAuthSess.Done():
		default:
			return s.noAuthSess, nil
		}
	}
	sess, _, err := s.createSession(s.baseCtx, s.devUser, false)
	if err != nil {
		return nil, err
	}
	s.noAuthSess = sess
	return sess, nil
}

func (s *Server) spec(a *account.Account) *bridge.Spec {
	return &bridge.Spec{
		Bridge:     s.opts.Bridge,
		Config:     s.opts.ConfigPath,
		Account:    a,
		SwitchUser: !s.opts.Dev,
		Logger:     s.log,
	}
}

// createSession starts the user bridge and registers a new session.
func (s *Server) createSession(ctx context.Context, a *account.Account, remember bool) (*Session, string, error) {
	p, err := bridge.StartUser(ctx, s.spec(a))
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	sess := &Session{Account: a, Created: now, Remember: remember, lastSeen: now, user: p}
	token, err := s.sessions.add(sess)
	if err != nil {
		p.Stop()
		return nil, "", err
	}
	return sess, token, nil
}

func (s *Server) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     brand.SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !s.opts.Dev,
		SameSite: http.SameSiteStrictMode,
	})
}

type loginRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}

// userInfo is shared by the login and session responses.
type userInfo struct {
	User          string   `json:"user"`
	Name          string   `json:"name"`
	UID           uint32   `json:"uid"`
	Home          string   `json:"home"`
	Groups        []string `json:"groups"`
	IsRoot        bool     `json:"isRoot"`
	IsAdmin       bool     `json:"isAdmin"`
	CanSudo       bool     `json:"canSudo"`
	UnlockedUntil *int64   `json:"unlockedUntil,omitempty"`
}

func (s *Server) info(sess *Session) userInfo {
	a := sess.Account
	ui := userInfo{User: a.Name, Name: a.FullName, UID: a.UID, Home: a.Home, Groups: a.GroupNames,
		IsRoot: a.IsRoot(), CanSudo: a.CanSudo()}
	if ui.Groups == nil {
		ui.Groups = []string{}
	}
	if until := sess.unlockedUntil(s.Config().Session.AdminUnlock.Duration); !until.IsZero() && time.Now().Before(until) {
		ms := until.UnixMilli()
		ui.UnlockedUntil = &ms
	}
	ui.IsAdmin = ui.IsRoot || ui.UnlockedUntil != nil
	return ui
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if e := decodeJSON(w, r, 16<<10, &req); e != nil {
		writeError(w, e)
		return
	}
	cfg := s.Config()
	ip := clientIP(r)
	if blocked, wait := s.limiter.blocked(ip, cfg.Login.MaxFailures); blocked {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		writeErrorStatus(w, http.StatusTooManyRequests, rpc.Errorf(rpc.Forbidden, "too many failed attempts, try again later").
			WithData(map[string]any{"reason": "rate_limited", "retryAfter": int(wait.Seconds()) + 1}))
		return
	}
	if !account.ValidName(req.User) || req.Password == "" || len(req.Password) > 4096 {
		s.limiter.fail(ip)
		writeError(w, errBadCredentials)
		return
	}
	if req.User == "root" && !cfg.AllowRoot {
		writeError(w, rpc.Errorf(rpc.Forbidden, "signing in as root is disabled").WithData(map[string]string{"reason": "root_disabled"}))
		return
	}
	if s.opts.Dev && req.User != s.devUser.Name {
		s.limiter.fail(ip)
		writeError(w, rpc.Errorf(rpc.Forbidden, "in dev mode only %s can sign in", s.devUser.Name).WithData(map[string]string{"reason": "dev_mode_user"}))
		return
	}

	select {
	case s.pamSem <- struct{}{}:
	case <-r.Context().Done():
		return
	}
	err := pam.Authenticate(pam.Service(), req.User, req.Password, ip)
	<-s.pamSem
	if err != nil {
		switch {
		case errors.Is(err, pam.ErrAccount):
			s.log.Printf("login %q from %s refused by account check: %v", req.User, ip, err)
			writeError(w, rpc.Errorf(rpc.Forbidden, "this account cannot sign in right now").WithData(map[string]string{"reason": "account"}))
		case errors.Is(err, pam.ErrAuth):
			s.limiter.fail(ip)
			s.log.Printf("login %q from %s failed", req.User, ip)
			writeError(w, errBadCredentials)
		default:
			s.log.Printf("login %q: %v", req.User, err)
			writeError(w, rpc.Errorf(rpc.Internal, "authentication service error"))
		}
		return
	}

	a, err := account.Lookup(req.User)
	if err != nil {
		s.log.Printf("login %q: lookup: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Internal, "cannot resolve the account"))
		return
	}
	if a.IsRoot() && !cfg.AllowRoot {
		writeError(w, rpc.Errorf(rpc.Forbidden, "signing in as root is disabled").WithData(map[string]string{"reason": "root_disabled"}))
		return
	}
	s.limiter.reset(ip)
	sess, token, err := s.createSession(r.Context(), a, req.Remember)
	if err != nil {
		s.log.Printf("login %q: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "could not start the session"))
		return
	}
	maxAge := 0
	if req.Remember {
		maxAge = int(cfg.Session.Timeout.Seconds())
	}
	s.setCookie(w, token, maxAge)
	s.log.Printf("login %q from %s", a.Name, ip)
	writeJSON(w, http.StatusOK, s.info(sess))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(brand.SessionCookie); err == nil {
		if sess := s.sessions.get(c.Value); sess != nil {
			s.sessions.remove(sess)
		}
	}
	s.setCookie(w, "", -1)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, sess *Session) {
	writeJSON(w, http.StatusOK, s.info(sess))
}

func (s *Server) handleUnlock(w http.ResponseWriter, r *http.Request, sess *Session) {
	var req struct {
		Password string `json:"password"`
	}
	if e := decodeJSON(w, r, 16<<10, &req); e != nil {
		writeError(w, e)
		return
	}
	if sess.Account.IsRoot() {
		writeJSON(w, http.StatusOK, struct{}{}) // root needs no unlock
		return
	}
	ip := clientIP(r)
	cfg := s.Config()
	if blocked, wait := s.limiter.blocked(ip, cfg.Login.MaxFailures); blocked {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		writeErrorStatus(w, http.StatusTooManyRequests, rpc.Errorf(rpc.Forbidden, "too many failed attempts, try again later").
			WithData(map[string]any{"reason": "rate_limited", "retryAfter": int(wait.Seconds()) + 1}))
		return
	}
	sess.mu.Lock()
	if sess.unlocking {
		sess.mu.Unlock()
		writeError(w, rpc.Errorf(rpc.Conflict, "unlock already in progress"))
		return
	}
	sess.unlocking = true
	sess.mu.Unlock()
	defer func() {
		sess.mu.Lock()
		sess.unlocking = false
		sess.mu.Unlock()
	}()

	p, err := bridge.StartAdmin(r.Context(), s.spec(sess.Account), req.Password)
	if err != nil {
		e := rpc.ToError(err, false)
		if e.Code == rpc.Invalid {
			s.limiter.fail(ip)
		}
		s.log.Printf("unlock for %q from %s failed: %v", sess.Account.Name, ip, e)
		writeError(w, e)
		return
	}
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		p.Stop()
		writeError(w, errUnauthenticated)
		return
	}
	old := sess.root
	sess.root = p
	sess.rootUsed = time.Now()
	sess.mu.Unlock()
	if old != nil {
		go old.Stop()
	}
	s.log.Printf("admin rights unlocked for %q from %s", sess.Account.Name, ip)
	until := time.Now().Add(cfg.Session.AdminUnlock.Duration).UnixMilli()
	writeJSON(w, http.StatusOK, map[string]int64{"unlockedUntil": until})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request, sess *Session) {
	sess.lock()
	writeJSON(w, http.StatusOK, struct{}{})
}

func itoa(n int) string { return strconv.Itoa(max(n, 0)) }
