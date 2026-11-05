package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/audit"
	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sshauth"
	"golang.org/x/crypto/ssh"
)

// keyFailMinDelay is the least time a refused SSH-key sign-in takes, so the
// answer time does not tell an unknown user from a key that is not listed
// (a password failure is slowed down by PAM in the same way).
var keyFailMinDelay = time.Second

var (
	errKeyRefused = rpc.Errorf(rpc.Unauthenticated, "this key is not accepted for this user").
			WithData(map[string]string{"reason": "key_refused"})
	errChallengeInvalid = rpc.Errorf(rpc.Unauthenticated, "the sign-in challenge expired or was already used, try again").
				WithData(map[string]string{"reason": "challenge_invalid"})
	errKeysDisabled = rpc.Errorf(rpc.Forbidden, "signing in with an SSH key is disabled on this server").
			WithData(map[string]string{"reason": "ssh_keys_disabled"})
	errRootDisabled = rpc.Errorf(rpc.Forbidden, "signing in as root is disabled").
			WithData(map[string]string{"reason": "root_disabled"})
)

// signedHost returns the host name the browser says it used, when this
// server answers to it (same rules as the Origin check); "" otherwise.
// Without one, the request's Host is used.
func (s *Server) signedHost(host string, r *http.Request) string {
	if host == "" {
		host = r.Host
	}
	if len(host) > 255 || strings.ContainsAny(host, "/\\@?#\n\r ") {
		return ""
	}
	for _, scheme := range []string{"https", "http"} {
		if s.originAllowed(scheme+"://"+host, r) {
			return host
		}
	}
	return ""
}

// handleChallenge answers POST /api/auth/challenge {user, host}: a
// one-time nonce for an SSH-key sign-in, valid 60 s, bound to the user,
// the client address and the host. It answers the same way whether the
// user exists or not.
func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string `json:"user"`
		Host string `json:"host"`
	}
	if e := decodeJSON(w, r, 4<<10, &req); e != nil {
		writeError(w, e)
		return
	}
	cfg := s.Config()
	if e := keySignInClosed(cfg, req.User); e != nil {
		writeError(w, e)
		return
	}
	if !account.ValidName(req.User) {
		writeError(w, rpc.Errorf(rpc.Invalid, "invalid user name"))
		return
	}
	ip := s.realClientIP(r)
	key := limiterKey(ip)
	if blocked, wait := s.limiter.blocked(key, cfg.Login.MaxFailures); blocked {
		writeRejected(w, &rejection{wait: wait})
		return
	}
	host := s.signedHost(req.Host, r)
	if host == "" {
		writeError(w, rpc.Errorf(rpc.Invalid, "this server does not answer to host %q (see web.allowed_origins)", req.Host).
			WithData(map[string]string{"reason": "host_not_allowed"}))
		return
	}
	c, err := s.challenges.Issue(req.User, ip, key, host)
	if err != nil {
		if errors.Is(err, sshauth.ErrFull) {
			writeError(w, errBusy)
			return
		}
		writeError(w, rpc.Errorf(rpc.Internal, "cannot create a challenge"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nonce":     c.Nonce,
		"challenge": string(sshauth.Message(c.Host, c.User, c.Nonce)),
		"host":      c.Host,
		"expires":   c.Expires.UnixMilli(),
	})
}

type keyLoginRequest struct {
	User      string `json:"user"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	Nonce     string `json:"nonce"`
	Remember  bool   `json:"remember"`
}

// keyAuthorized checks pub against a's authorized_keys (read as a, with
// sshd's StrictModes rules); in --dev with --dev-authorized-keys that
// file is used instead. fromIP is the client address from= options are
// matched against; "" when it is not known (from= lines then refuse).
func (s *Server) keyAuthorized(a *account.Account, pub ssh.PublicKey, fromIP string) error {
	ip := net.ParseIP(fromIP)
	if s.opts.Dev && s.opts.DevAuthorizedKeys != "" {
		data, err := readDevAuthorizedKeys(s.opts.DevAuthorizedKeys)
		if err != nil {
			return err
		}
		_, err = sshauth.FindKey(data, pub, ip, time.Now())
		return err
	}
	_, err := sshauth.Authorize(sshauth.User{Name: a.Name, UID: a.UID, GID: a.GID, Home: a.Home, Groups: a.Groups, SID: a.SID, Admin: a.CanSudo()}, pub, ip, time.Now())
	return err
}

// readDevAuthorizedKeys reads the --dev-authorized-keys file after
// checkDevAuthorizedKeys (at most 1 MiB).
func readDevAuthorizedKeys(path string) ([]byte, error) {
	f, err := openDevAuthorizedKeys(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := checkDevAuthorizedKeys(f); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, errors.New("--dev-authorized-keys: larger than 1 MiB")
	}
	return b, nil
}

// handleLoginKey answers POST /api/auth/login-key: it signs the user in
// when the signature over the challenge verifies with a key listed in the
// user's authorized_keys and PAM account management accepts the account
// (no pam_authenticate: the key replaces the password). Every refusal that
// could tell whether the user exists gets the same answer and delay.
func (s *Server) handleLoginKey(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req keyLoginRequest
	if e := decodeJSON(w, r, 32<<10, &req); e != nil {
		writeError(w, e)
		return
	}
	cfg := s.Config()
	if e := keySignInClosed(cfg, req.User); e != nil {
		writeError(w, e)
		return
	}
	ip := s.realClientIP(r)
	att, rej := s.limiter.begin(limiterKey(ip), cfg.Login.MaxFailures)
	if rej != nil {
		writeRejected(w, rej)
		return
	}
	result := attemptFailed
	defer func() { att.done(result) }()
	// A name that is not a valid user name is never written to the log or
	// the activity log as given (it is the caller's text).
	who := req.User
	if !account.ValidName(who) {
		who = ""
	}
	fail := func(f *keyLoginFailure) {
		if !f.refused {
			result = attemptNeutral
			writeError(w, f.err)
			return
		}
		s.log.Printf("login %q from %s method=ssh-key refused: %s", who, ip, f.why)
		s.auditCore(who, ip, "login.failed", "ssh-key", audit.Denied, f.why)
		sleepUntil(r.Context(), start.Add(keyFailMinDelay))
		writeError(w, f.err)
	}

	if !account.ValidName(req.User) {
		fail(keyRefusal(errKeyRefused, "invalid user name"))
		return
	}
	if s.opts.Dev && req.User != s.devUser.Name {
		writeError(w, rpc.Errorf(rpc.Forbidden, "in dev mode only %s can sign in", s.devUser.Name).WithData(map[string]string{"reason": "dev_mode_user"}))
		return
	}
	pub, fp, f := s.verifyKeySignature(&req, ip)
	if f != nil {
		fail(f)
		return
	}
	a, fromIP, f := s.keyAccount(r, cfg.AllowRoot, req.User, pub, fp)
	if f != nil {
		fail(f)
		return
	}
	// The key is proven: the allowlist refusal can be explicit.
	if !signInAllowed(cfg, a) {
		s.log.Printf("login %q from %s method=ssh-key refused: not allowed by auth.allow_users / auth.allow_groups / auth.admins_only", req.User, ip)
		writeError(w, errNotAllowed)
		return
	}
	if f := s.checkKeyAccount(r.Context(), a, req.User, ip); f != nil {
		fail(f)
		return
	}
	result = attemptOK
	sess, token, err := s.createSession(r.Context(), a, req.Remember, ip, &sessionKey{pub: pub, fingerprint: fp, fromIP: fromIP})
	if err != nil {
		s.log.Printf("login %q method=ssh-key: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "could not start the session"))
		return
	}
	s.setSessionCookie(w, r, token, req.Remember, cfg)
	s.log.Printf("login %q from %s method=ssh-key key=%s", a.Name, ip, fp)
	s.auditCore(a.Name, ip, "login", "ssh-key "+fp, audit.OK, "")
	writeJSON(w, http.StatusOK, s.info(sess))
}

// keySignInClosed is the answer when key sign-in is off, or when user is
// root and root may not sign in; nil otherwise.
func keySignInClosed(cfg *config.Config, user string) *rpc.Error {
	if !cfg.Auth.SSHKeys {
		return errKeysDisabled
	}
	if user == "root" && !cfg.AllowRoot {
		return errRootDisabled
	}
	return nil
}

// keyLoginFailure ends a key sign-in. A refused one counts as a failed
// attempt and is logged, audited and answered no sooner than
// keyFailMinDelay; any other is a server-side problem that does not count.
type keyLoginFailure struct {
	err     *rpc.Error
	why     string
	refused bool
}

func keyRefusal(e *rpc.Error, why string) *keyLoginFailure {
	return &keyLoginFailure{err: e, why: why, refused: true}
}

func keyServerError(e *rpc.Error) *keyLoginFailure {
	return &keyLoginFailure{err: e}
}

// sleepUntil waits until t, or less when ctx ends first.
func sleepUntil(ctx context.Context, t time.Time) {
	d := time.Until(t)
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
	}
}

// verifyKeySignature burns the challenge req.Nonce and checks req.Signature
// over it with req.PublicKey; it returns the key and its fingerprint.
func (s *Server) verifyKeySignature(req *keyLoginRequest, ip string) (ssh.PublicKey, string, *keyLoginFailure) {
	ch, err := s.challenges.Take(req.Nonce, req.User, ip)
	if err != nil {
		return nil, "", keyRefusal(errChallengeInvalid, "challenge unknown, used, expired or issued to another user/address")
	}
	pub, err := sshauth.ParsePublicKey(req.PublicKey)
	if err != nil {
		return nil, "", keyRefusal(rpc.Errorf(rpc.Invalid, "this key type is not supported (use ed25519, ECDSA or RSA of at least 2048 bits)").
			WithData(map[string]string{"reason": "unsupported_key"}), err.Error())
	}
	fp := sshauth.Fingerprint(pub)
	sig, err := sshauth.ParseSignature(req.Signature)
	if err == nil {
		err = sshauth.Verify(pub, sig, sshauth.Message(ch.Host, req.User, req.Nonce))
	}
	if err != nil {
		return nil, "", keyRefusal(errKeyRefused, "key "+fp+": "+err.Error())
	}
	return pub, fp, nil
}

// keyAccount resolves user and checks that the account may sign in and
// lists pub; it also returns the client address from= options were
// matched against ("" when not known).
func (s *Server) keyAccount(r *http.Request, allowRoot bool, user string, pub ssh.PublicKey, fp string) (*account.Account, string, *keyLoginFailure) {
	a, err := account.Lookup(user)
	if err != nil {
		if unknownUser(err) {
			return nil, "", keyRefusal(errKeyRefused, "no such user")
		}
		s.log.Printf("login %q method=ssh-key: lookup: %v", user, err)
		return nil, "", keyServerError(rpc.Errorf(rpc.Internal, "cannot resolve the account"))
	}
	if a.IsRoot() && !allowRoot {
		return nil, "", keyRefusal(errKeyRefused, "uid 0 and allow_root = false")
	}
	if !account.ShellAllowed(a.Shell) {
		return nil, "", keyRefusal(errKeyRefused, "login shell "+a.Shell+" is not allowed (nologin, restricted or not in /etc/shells)")
	}
	// from= is matched against the browser's address only when it is known
	// (not a trusted proxy that sent no X-Forwarded-For).
	fromIP, known := s.clientAddr(r)
	if !known {
		fromIP = ""
	}
	if err := s.keyAuthorized(a, pub, fromIP); err != nil {
		return nil, "", keyRefusal(errKeyRefused, "key "+fp+": "+err.Error())
	}
	return a, fromIP, nil
}

// checkKeyAccount runs PAM account management (expired/locked accounts,
// pam_access…) without pam_authenticate, then gets the logon token the
// bridge needs on Windows. user is the name as given, for the log.
func (s *Server) checkKeyAccount(ctx context.Context, a *account.Account, user, ip string) *keyLoginFailure {
	release, e := s.pamSlot(ctx)
	if e != nil {
		return keyServerError(e)
	}
	err := s.pamAccount(a.Name, ip)
	release()
	if err != nil {
		if errors.Is(err, pam.ErrAccount) {
			return keyRefusal(errKeyRefused, "PAM account check: "+err.Error())
		}
		s.log.Printf("login %q method=ssh-key: %v", user, err)
		return keyServerError(rpc.Errorf(rpc.Internal, "authentication service error"))
	}
	// Windows: a key sign-in has no password, so the token is obtained with
	// an S4U logon (no-op elsewhere).
	if err := s.keyLogonToken(a.Name); err != nil {
		if errors.Is(err, pam.ErrAccount) || errors.Is(err, pam.ErrAuth) {
			return keyRefusal(errKeyRefused, "token: "+err.Error())
		}
		s.log.Printf("login %q method=ssh-key: %v", user, err)
		return keyServerError(rpc.Errorf(rpc.Internal, "authentication service error"))
	}
	return nil
}

// pamAccount runs PAM account management for name (replaceable in tests).
func (s *Server) pamAccount(name, rhost string) error {
	if s.pamAcctHook != nil {
		return s.pamAcctHook(name, rhost)
	}
	return pam.CheckAccount(pam.Service(), name, rhost)
}
