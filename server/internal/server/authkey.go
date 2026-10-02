package server

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/pam"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sshauth"
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
	if !cfg.Auth.SSHKeys {
		writeError(w, errKeysDisabled)
		return
	}
	if req.User == "root" && !cfg.AllowRoot {
		writeError(w, errRootDisabled)
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
// file is used instead.
func (s *Server) keyAuthorized(a *account.Account, pub ssh.PublicKey, rhost string) error {
	ip := net.ParseIP(rhost)
	if s.opts.Dev && s.opts.DevAuthorizedKeys != "" {
		data, err := os.ReadFile(s.opts.DevAuthorizedKeys)
		if err != nil {
			return err
		}
		_, err = sshauth.FindKey(data, pub, ip, time.Now())
		return err
	}
	_, err := sshauth.Authorize(sshauth.User{Name: a.Name, UID: a.UID, GID: a.GID, Home: a.Home}, pub, ip, time.Now())
	return err
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
	if !cfg.Auth.SSHKeys {
		writeError(w, errKeysDisabled)
		return
	}
	if req.User == "root" && !cfg.AllowRoot {
		writeError(w, errRootDisabled)
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
	refuse := func(e *rpc.Error, why string) {
		s.log.Printf("login %q from %s method=ssh-key refused: %s", req.User, ip, why)
		if d := time.Until(start.Add(keyFailMinDelay)); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-r.Context().Done():
				t.Stop()
			}
		}
		writeError(w, e)
	}

	if !account.ValidName(req.User) {
		refuse(errKeyRefused, "invalid user name")
		return
	}
	if s.opts.Dev && req.User != s.devUser.Name {
		writeError(w, rpc.Errorf(rpc.Forbidden, "in dev mode only %s can sign in", s.devUser.Name).WithData(map[string]string{"reason": "dev_mode_user"}))
		return
	}
	ch, err := s.challenges.Take(req.Nonce, req.User, ip)
	if err != nil {
		refuse(errChallengeInvalid, "challenge unknown, used, expired or issued to another user/address")
		return
	}
	pub, err := sshauth.ParsePublicKey(req.PublicKey)
	if err != nil {
		refuse(rpc.Errorf(rpc.Invalid, "this key type is not supported (use ed25519, ECDSA or RSA of at least 2048 bits)").
			WithData(map[string]string{"reason": "unsupported_key"}), err.Error())
		return
	}
	fp := sshauth.Fingerprint(pub)
	sig, err := sshauth.ParseSignature(req.Signature)
	if err == nil {
		err = sshauth.Verify(pub, sig, sshauth.Message(ch.Host, req.User, req.Nonce))
	}
	if err != nil {
		refuse(errKeyRefused, "key "+fp+": "+err.Error())
		return
	}

	a, err := account.Lookup(req.User)
	if err != nil {
		if unknownUser(err) {
			refuse(errKeyRefused, "no such user")
			return
		}
		result = attemptNeutral
		s.log.Printf("login %q method=ssh-key: lookup: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Internal, "cannot resolve the account"))
		return
	}
	if a.IsRoot() && !cfg.AllowRoot {
		refuse(errKeyRefused, "uid 0 and allow_root = false")
		return
	}
	if !account.ShellAllowed(a.Shell) {
		refuse(errKeyRefused, "login shell "+a.Shell+" is not allowed (nologin, restricted or not in /etc/shells)")
		return
	}
	if err := s.keyAuthorized(a, pub, ip); err != nil {
		refuse(errKeyRefused, "key "+fp+": "+err.Error())
		return
	}
	// PAM account management (expired/locked accounts, pam_access…),
	// without pam_authenticate.
	release, e := s.pamSlot(r.Context())
	if e != nil {
		result = attemptNeutral
		writeError(w, e)
		return
	}
	err = s.pamAccount(a.Name, ip)
	release()
	if err != nil {
		if errors.Is(err, pam.ErrAccount) {
			refuse(errKeyRefused, "PAM account check: "+err.Error())
			return
		}
		result = attemptNeutral
		s.log.Printf("login %q method=ssh-key: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Internal, "authentication service error"))
		return
	}
	result = attemptOK
	sess, token, err := s.createSession(r.Context(), a, req.Remember, ip, &sessionKey{pub: pub, fingerprint: fp})
	if err != nil {
		s.log.Printf("login %q method=ssh-key: %v", req.User, err)
		writeError(w, rpc.Errorf(rpc.Unavailable, "could not start the session"))
		return
	}
	maxAge := 0
	if req.Remember {
		maxAge = int(cfg.Session.Timeout.Seconds())
	}
	s.setCookie(w, token, maxAge)
	s.log.Printf("login %q from %s method=ssh-key key=%s", a.Name, ip, fp)
	writeJSON(w, http.StatusOK, s.info(sess))
}

// pamAccount runs PAM account management for name (replaceable in tests).
func (s *Server) pamAccount(name, rhost string) error {
	if s.pamAcctHook != nil {
		return s.pamAcctHook(name, rhost)
	}
	return pam.CheckAccount(pam.Service(), name, rhost)
}
