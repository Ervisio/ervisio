package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

var (
	errNotFound        = rpc.Errorf(rpc.NotFound, "not found")
	errUnauthenticated = rpc.Errorf(rpc.Unauthenticated, "not signed in")
	errBadCredentials  = rpc.Errorf(rpc.Unauthenticated, "wrong user name or password")
	// errNotAllowed is only returned to SSH-key sign-ins, after the key
	// signature verified and authorized_keys lists the key: whoever sees it
	// holds a key of the account. Password sign-ins get errBadCredentials (no
	// password oracle, like the other refusals after PAM accepted it).
	errNotAllowed = rpc.Errorf(rpc.Forbidden, "This account may not sign in to "+brand.Name).
			WithData(map[string]string{"reason": "not_allowed"})
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
	// The LinuxAdmin value is still accepted: a tab that loaded the web app
	// before the rename keeps working until it reloads.
	if v := r.Header.Get(brand.CSRFHeader); v != brand.CSRFValue && v != brand.LegacyCSRFValue {
		return rpc.Errorf(rpc.Forbidden, "missing %s: %s header", brand.CSRFHeader, brand.CSRFValue)
	}
	if o := r.Header.Get("Origin"); o != "" && !s.originAllowed(o, r) {
		s.log.Printf("refused request from origin %q to host %q (peer %s): add the origin to web.allowed_origins or set web.trusted_proxies for a reverse proxy", o, r.Host, clientIP(r))
		return rpc.Errorf(rpc.Forbidden, "cross-origin request refused: this page was opened as %s, which the server does not recognise. See web.allowed_origins in the configuration.", o)
	}
	return nil
}

// originAllowed accepts the host the browser used (the Host header, or
// X-Forwarded-Host from a trusted reverse proxy), the origins listed in
// web.allowed_origins and, in dev mode, the configured Vite dev server
// (--vite). Other local ports are refused: the session cookie is shared by
// every port of the host.
func (s *Server) originAllowed(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	oh := config.NormalizeHost(u.Host, u.Scheme)
	for _, h := range s.requestHosts(r) {
		if oh == config.NormalizeHost(h, u.Scheme) {
			return true
		}
	}
	norm := u.Scheme + "://" + oh
	for _, a := range s.Config().Web.AllowedOrigins {
		if n, err := config.ParseOrigin(a); err == nil && n == norm {
			return true
		}
	}
	return s.opts.Dev && s.viteHost != "" && strings.EqualFold(u.Host, s.viteHost)
}

// requestHosts are the host names this request was addressed to: the Host
// header and, when the peer is a trusted proxy, X-Forwarded-Host.
func (s *Server) requestHosts(r *http.Request) []string {
	hosts := []string{r.Host}
	if s.fromTrustedProxy(r) {
		if fh := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); fh != "" {
			hosts = append(hosts, fh)
		}
	}
	return hosts
}

// loopbackHost reports whether a Host header names this machine's loopback
// interface: localhost, 127.0.0.0/8 or [::1], with any port.
func loopbackHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	} else if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// devHostCheck (dev mode) refuses requests whose Host header is not a
// loopback name or address, so a DNS-rebinding page cannot talk to the
// development daemon through the browser.
func (s *Server) devHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			writeErrorStatus(w, http.StatusMisdirectedRequest, rpc.Errorf(rpc.Forbidden, "dev mode only answers to localhost, 127.0.0.1 or [::1]"))
			return
		}
		next.ServeHTTP(w, r)
	})
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
	c, err := r.Cookie(brand.SessionCookie)
	if err != nil {
		return nil
	}
	sess := s.sessions.get(c.Value)
	if sess == nil {
		return nil
	}
	now := time.Now()
	sess.mu.Lock()
	idle := now.Sub(sess.lastSeen)
	expired := !sess.expires.IsZero() && now.After(sess.expires)
	sess.mu.Unlock()
	if idle <= s.Config().Session.Timeout.Duration && !expired {
		return sess
	}
	s.sessions.remove(sess)
	return nil
}

// newNoAuthToken (--dev-insecure-noauth) makes a fresh one-time sign-in
// token and returns the URL that redeems it.
func (s *Server) newNoAuthToken(base string) (string, error) {
	tok, err := newToken()
	if err != nil {
		return "", err
	}
	s.noAuthMu.Lock()
	s.noAuthToken = tok
	s.noAuthMu.Unlock()
	return base + "/api/dev/noauth?token=" + tok, nil
}

// handleNoAuth (--dev-insecure-noauth) redeems the one-time start-up token
// printed on the console: it signs the browser in as the daemon's user and
// redirects to the app. Each token works once; a new one is printed.
func (s *Server) handleNoAuth(w http.ResponseWriter, r *http.Request) {
	got := r.URL.Query().Get("token")
	s.noAuthMu.Lock()
	want := s.noAuthToken
	ok := want != "" && len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
	if ok {
		s.noAuthToken = ""
	}
	s.noAuthMu.Unlock()
	if !ok {
		writeError(w, rpc.Errorf(rpc.Forbidden, "invalid or already used dev sign-in token (see the daemon's console)"))
		return
	}
	sess, token, err := s.createSession(s.baseCtx, s.devUser, false, clientIP(r), nil)
	if err != nil {
		s.log.Printf("noauth session: %v", err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "could not start the session"))
		return
	}
	s.setCookie(w, r, token, 0)
	s.log.Printf("--dev-insecure-noauth: signed a browser in as %q", sess.Account.Name)
	if u, err := s.newNoAuthToken("http://" + r.Host); err == nil {
		s.log.Printf("--dev-insecure-noauth: next one-time sign-in URL: %s", u)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) spec(a *account.Account, rhost string) *bridge.Spec {
	sp := &bridge.Spec{
		Bridge:         s.opts.Bridge,
		Config:         s.opts.ConfigPath,
		Account:        a,
		SwitchUser:     !s.opts.Dev,
		Dev:            s.opts.Dev,
		DevPlugins:     s.opts.DevPluginsDir,
		DevPluginsFile: s.opts.DevPluginsFile,
		RHost:          rhost,
		Logger:         s.log,
	}
	if !s.opts.Dev {
		sp.SessionHelper = s.opts.SessionHelper
	}
	return sp
}

// sessionLifetime is the absolute lifetime of a session, whatever its
// activity: 24 hours, or session.timeout for "stay signed in" sessions
// when that is longer.
func sessionLifetime(remember bool, timeout time.Duration) time.Duration {
	const base = 24 * time.Hour
	if remember && timeout > base {
		return timeout
	}
	return base
}

// createSession starts the user bridge and registers a new session. key
// is the SSH key used to sign in, nil for a password sign-in.
func (s *Server) createSession(ctx context.Context, a *account.Account, remember bool, rhost string, key *sessionKey) (*Session, string, error) {
	p, err := bridge.StartUser(ctx, s.spec(a, rhost))
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	method := "password"
	if key != nil {
		method = "ssh-key"
	}
	sess := &Session{Account: a, Created: now, Remember: remember, RHost: rhost, Method: method, sshKey: key, lastSeen: now, user: p,
		expires: now.Add(sessionLifetime(remember, s.Config().Session.Timeout.Duration)), checked: now}
	sess.shadowFP = s.checker.shadowFingerprint(a.Name)
	token, err := s.sessions.add(sess)
	if err != nil {
		p.Stop()
		return nil, "", err
	}
	return sess, token, nil
}

// secureCookies reports whether the session cookie gets the Secure flag.
// Always, except in dev mode and for plain-HTTP requests in tls.mode =
// "http", where it is decided per request, failing closed:
//   - a trusted reverse proxy (web.trusted_proxies) that says
//     X-Forwarded-Proto: Secure only for "https";
//   - otherwise (no such header, or a peer that is not a trusted proxy,
//     whose header is ignored): Secure unless the browser addressed a
//     loopback name (http://127.0.0.1:PORT, an SSH tunnel to localhost).
//
// A request that arrived over TLS is always Secure, also while a changed
// tls.mode waits for a restart (the configured mode is not the served one).
func (s *Server) secureCookies(r *http.Request) bool {
	if s.opts.Dev {
		return false
	}
	if r == nil || r.TLS != nil || s.Config().TLS.Mode != config.TLSHTTP {
		return true
	}
	if s.fromTrustedProxy(r) {
		if p := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); p != "" {
			return strings.EqualFold(p, "https")
		}
	}
	return !loopbackHost(r.Host)
}

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     brand.SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookies(r),
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
	// UnlockedForever is set while unlocked with session.admin_unlock = 0:
	// admin rights last until sign-out, unlockedUntil is the session end.
	UnlockedForever bool `json:"unlockedForever,omitempty"`
	// AuthMethod is how this session signed in: "password" or "ssh-key".
	AuthMethod string `json:"authMethod"`
	// KeyFingerprint is the SHA256 fingerprint of the SSH key used to sign
	// in (authMethod "ssh-key" only).
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
}

func (s *Server) info(sess *Session) userInfo {
	a := sess.Account
	ui := userInfo{User: a.Name, Name: a.FullName, UID: a.UID, Home: a.Home, Groups: a.GroupNames,
		IsRoot: a.IsRoot(), CanSudo: a.CanSudo(), AuthMethod: sess.Method}
	if ui.AuthMethod == "" {
		ui.AuthMethod = "password"
	}
	if sess.sshKey != nil {
		ui.KeyFingerprint = sess.sshKey.fingerprint
	}
	if ui.Groups == nil {
		ui.Groups = []string{}
	}
	if until := sess.unlockedUntil(s.Config().Session.AdminUnlock.Duration); !until.IsZero() && time.Now().Before(until) {
		ms := until.UnixMilli()
		ui.UnlockedUntil = &ms
		ui.UnlockedForever = s.Config().Session.AdminUnlock.Duration == 0
	}
	ui.IsAdmin = ui.IsRoot || ui.UnlockedUntil != nil
	return ui
}

// writeRejected answers a request refused by the limiter.
func writeRejected(w http.ResponseWriter, rej *rejection) {
	secs := int(rej.wait.Seconds()) + 1
	w.Header().Set("Retry-After", itoa(secs))
	msg, reason := "too many failed attempts, try again later", "rate_limited"
	if rej.busy {
		msg, reason = "another sign-in attempt is still running, try again in a moment", "busy"
	}
	writeErrorStatus(w, http.StatusTooManyRequests, rpc.Errorf(rpc.Forbidden, "%s", msg).
		WithData(map[string]any{"reason": reason, "retryAfter": secs}))
}

// pamSlotWait bounds how long a sign-in waits for a free PAM slot.
const pamSlotWait = 5 * time.Second

var errBusy = rpc.Errorf(rpc.Unavailable, "the server is busy checking other sign-ins, try again in a moment").
	WithData(map[string]string{"reason": "busy"})

// pamSlot takes one of the global PAM slots, waiting at most pamSlotWait.
func (s *Server) pamSlot(ctx context.Context) (release func(), e *rpc.Error) {
	t := time.NewTimer(pamSlotWait)
	defer t.Stop()
	select {
	case s.pamSem <- struct{}{}:
		return func() { <-s.pamSem }, nil
	case <-t.C:
		return nil, errBusy
	case <-ctx.Done():
		return nil, errBusy
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if e := decodeJSON(w, r, 16<<10, &req); e != nil {
		writeError(w, e)
		return
	}
	cfg := s.Config()
	ip := s.realClientIP(r)
	// Refused before any password check, so this reveals only the
	// configuration (the name "root" is public).
	if req.User == "root" && !cfg.AllowRoot {
		writeError(w, rpc.Errorf(rpc.Forbidden, "signing in as root is disabled").WithData(map[string]string{"reason": "root_disabled"}))
		return
	}
	// Reserve the attempt (counted as a failure until it succeeds) before
	// anything else: concurrent attempts cannot get past the limit.
	att, rej := s.limiter.begin(limiterKey(ip), cfg.Login.MaxFailures)
	if rej != nil {
		writeRejected(w, rej)
		return
	}
	result := attemptFailed
	defer func() { att.done(result) }()

	if !account.ValidName(req.User) || req.Password == "" || len(req.Password) > 4096 {
		writeError(w, errBadCredentials)
		return
	}
	if s.opts.Dev && req.User != s.devUser.Name {
		writeError(w, rpc.Errorf(rpc.Forbidden, "in dev mode only %s can sign in", s.devUser.Name).WithData(map[string]string{"reason": "dev_mode_user"}))
		return
	}

	release, e := s.pamSlot(r.Context())
	if e != nil {
		result = attemptNeutral
		writeError(w, e)
		return
	}
	err := pam.Authenticate(pam.Service(), req.User, req.Password, ip)
	release()
	if err != nil {
		switch {
		case errors.Is(err, pam.ErrAccount):
			// The password was right but the account may not sign in. The
			// client gets the generic answer (no password oracle for
			// locked or expired accounts); the reason goes to the log.
			s.log.Printf("login %q from %s refused by the PAM account check: %v", req.User, ip, err)
			s.auditCore(req.User, ip, "login.failed", "password", audit.Denied, "account refused")
			writeError(w, errBadCredentials)
		case errors.Is(err, pam.ErrAuth):
			s.log.Printf("login %q from %s failed", req.User, ip)
			s.auditCore(req.User, ip, "login.failed", "password", audit.Denied, "wrong password")
			writeError(w, errBadCredentials)
		default:
			result = attemptNeutral
			s.log.Printf("login %q: %v", req.User, err)
			writeError(w, rpc.Errorf(rpc.Internal, "authentication service error"))
		}
		return
	}

	a, err := account.Lookup(req.User)
	if err != nil {
		result = attemptNeutral
		s.log.Printf("login %q: lookup: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Internal, "cannot resolve the account"))
		return
	}
	// Same generic answer as a wrong password for the refusals below: they
	// come after PAM accepted the password.
	if a.IsRoot() && !cfg.AllowRoot {
		s.log.Printf("login %q from %s refused: uid 0 and allow_root = false", req.User, ip)
		writeError(w, errBadCredentials)
		return
	}
	if !account.ShellAllowed(a.Shell) {
		s.log.Printf("login %q from %s refused: login shell %q is not allowed (nologin, restricted or not in /etc/shells)", req.User, ip, a.Shell)
		writeError(w, errBadCredentials)
		return
	}
	// Same generic answer as the refusals above: an explicit "not allowed"
	// would tell whoever guesses passwords that this one is right.
	if !signInAllowed(cfg, a) {
		s.log.Printf("login %q from %s refused: not allowed by auth.allow_users / auth.allow_groups / auth.admins_only", req.User, ip)
		writeError(w, errBadCredentials)
		return
	}
	result = attemptOK
	sess, token, err := s.createSession(r.Context(), a, req.Remember, ip, nil)
	if err != nil {
		s.log.Printf("login %q: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "could not start the session"))
		return
	}
	maxAge := 0
	if req.Remember {
		maxAge = int(cfg.Session.Timeout.Seconds())
	}
	s.setCookie(w, r, token, maxAge)
	s.log.Printf("login %q from %s method=password", a.Name, ip)
	s.auditCore(a.Name, ip, "login", "password", audit.OK, "")
	writeJSON(w, http.StatusOK, s.info(sess))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(brand.SessionCookie); err == nil {
		if sess := s.sessions.get(c.Value); sess != nil {
			s.auditCore(sess.Account.Name, s.realClientIP(r), "logout", "", audit.OK, "")
			s.sessions.remove(sess)
		}
	}
	s.setCookie(w, r, "", -1)
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
	ip := s.realClientIP(r)
	cfg := s.Config()
	att, rej := s.limiter.begin(limiterKey(ip), cfg.Login.MaxFailures)
	if rej != nil {
		writeRejected(w, rej)
		return
	}
	result := attemptNeutral
	defer func() { att.done(result) }()
	// Re-check the account first: a locked, expired or removed account
	// must not get root rights through a still-open session.
	if reason := s.revalidate(sess, true); reason != "" {
		s.endSession(sess, reason)
		writeError(w, errUnauthenticated)
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

	// An empty password tries sudo without one (NOPASSWD rules): useful
	// for sessions signed in with an SSH key. sudo -n never runs PAM
	// authentication, so a refusal is not counted as a failed attempt.
	p, err := bridge.StartAdmin(r.Context(), s.spec(sess.Account, ip), req.Password)
	if err != nil {
		e := rpc.ToError(err, false)
		if e.Code == rpc.Invalid && req.Password == "" {
			e = rpc.Errorf(rpc.Invalid, "sudo needs this account's password").WithData(map[string]string{"reason": "password_required"})
		} else if e.Code == rpc.Invalid {
			result = attemptFailed
		}
		s.log.Printf("unlock for %q from %s failed: %v", sess.Account.Name, ip, e)
		s.auditCore(sess.Account.Name, ip, "unlock", "", audit.Denied, string(e.Code))
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
	result = attemptOK
	if old != nil {
		go old.Stop()
	}
	s.log.Printf("admin rights unlocked for %q from %s", sess.Account.Name, ip)
	s.auditCore(sess.Account.Name, ip, "unlock", "", audit.OK, "")
	idle := cfg.Session.AdminUnlock.Duration
	writeJSON(w, http.StatusOK, map[string]any{
		"unlockedUntil":   sess.unlockedUntil(idle).UnixMilli(),
		"unlockedForever": idle == 0,
	})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request, sess *Session) {
	sess.lock()
	s.log.Printf("admin rights locked by %q", sess.Account.Name)
	s.auditCore(sess.Account.Name, s.realClientIP(r), "lock", "", audit.OK, "")
	writeJSON(w, http.StatusOK, struct{}{})
}

func itoa(n int) string { return strconv.Itoa(max(n, 0)) }
