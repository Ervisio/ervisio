package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os/user"
	"slices"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/pam"
	"github.com/Fonlogen/LinuxAdmin/server/internal/sshauth"
	"golang.org/x/crypto/ssh"
)

// accountChecker holds the lookups used to re-validate live sessions
// (fields are replaceable in tests).
type accountChecker struct {
	lookup  func(name string) (*account.Account, error)
	shadow  func(name string) (*account.ShadowEntry, error)
	pamAcct func(name, rhost string) error
	// keyAuth checks that an SSH key may still sign the account in from
	// rhost (nil error) or returns why not; nil = not checked.
	keyAuth func(a *account.Account, pub ssh.PublicKey, rhost string) error
	now     func() time.Time
}

func newAccountChecker() *accountChecker {
	return &accountChecker{
		lookup:  account.Lookup,
		shadow:  account.ReadShadow,
		pamAcct: func(name, rhost string) error { return pam.CheckAccount(pam.Service(), name, rhost) },
		now:     time.Now,
	}
}

// shadowFingerprint returns the account's shadow fingerprint, or "" when
// the shadow file cannot be read (dev mode) or has no entry for it.
func (c *accountChecker) shadowFingerprint(name string) string {
	if c == nil {
		return ""
	}
	e, err := c.shadow(name)
	if err != nil {
		return ""
	}
	return e.Fingerprint
}

// unknownUser reports whether err means the account no longer exists.
func unknownUser(err error) bool {
	var uue user.UnknownUserError
	return errors.As(err, &uue)
}

// revalidate checks that sess's account may still be signed in. It returns
// "" when it may, or the reason to end the session:
//   - the account was removed or its uid changed;
//   - its login shell is no longer allowed (set to nologin…);
//   - it lost a group it had at sign-in;
//   - (shadow readable, i.e. the daemon runs as root) the password was
//     locked or changed, or the account expired;
//   - PAM account management refuses it: always when the shadow file is
//     not readable, and on full checks (unlock, bridge restart);
//   - (SSH-key sessions) the key is no longer in authorized_keys, or its
//     from=/expiry-time= options now refuse it.
//
// SSH-key sessions ignore a locked or changed password, as sshd does: an
// account with a locked password ("!" in shadow, usermod -L) may still
// use its keys; account expiry still ends the session.
//
// Lookup or PAM failures that do not say anything about the account
// (NSS or PAM service errors) keep the session.
func (s *Server) revalidate(sess *Session, full bool) string {
	c := s.checker
	if c == nil {
		return ""
	}
	name := sess.Account.Name
	a, err := c.lookup(name)
	switch {
	case unknownUser(err):
		return "the account was removed"
	case err != nil:
		s.log.Printf("revalidate %q: lookup: %v (session kept)", name, err)
		return ""
	}
	if a.UID != sess.Account.UID {
		return fmt.Sprintf("the account's uid changed (%d → %d)", sess.Account.UID, a.UID)
	}
	if !account.ShellAllowed(a.Shell) {
		return fmt.Sprintf("the account's login shell %q is no longer allowed", a.Shell)
	}
	for _, g := range sess.Account.Groups {
		if !slices.Contains(a.Groups, g) {
			return fmt.Sprintf("the account left group %d", g)
		}
	}
	runPAM := full
	sh, err := c.shadow(name)
	switch {
	case err == nil:
		if sh.Locked && sess.sshKey == nil {
			return "the account's password is locked"
		}
		if sh.Expired(c.now()) {
			return "the account expired"
		}
		sess.mu.Lock()
		fp := sess.shadowFP
		sess.mu.Unlock()
		if fp != "" && sh.Fingerprint != fp && sess.sshKey == nil {
			return "the account's password changed"
		}
	case errors.Is(err, account.ErrNoShadowEntry):
		// Not a local shadow account (LDAP, homed…): ask PAM.
		runPAM = true
	case errors.Is(err, fs.ErrPermission), errors.Is(err, fs.ErrNotExist):
		runPAM = true
	default:
		s.log.Printf("revalidate %q: shadow: %v", name, err)
		runPAM = true
	}
	if sess.sshKey != nil && c.keyAuth != nil {
		if err := c.keyAuth(a, sess.sshKey.pub, sess.RHost); err != nil {
			if errors.Is(err, sshauth.ErrNotAuthorized) {
				return fmt.Sprintf("the SSH key %s is no longer authorized: %v", sess.sshKey.fingerprint, err)
			}
			s.log.Printf("revalidate %q: key check: %v (session kept)", name, err)
		}
	}
	if runPAM {
		if err := c.pamAcct(name, sess.RHost); err != nil {
			if errors.Is(err, pam.ErrAccount) {
				return fmt.Sprintf("PAM account check refused the account: %v", err)
			}
			s.log.Printf("revalidate %q: %v (session kept)", name, err)
		}
	}
	sess.mu.Lock()
	sess.checked = c.now()
	sess.mu.Unlock()
	return ""
}

// endSession closes sess (bridges stopped, WebSockets closed with 1008).
func (s *Server) endSession(sess *Session, reason string) {
	s.log.Printf("session of %q ended: %s", sess.Account.Name, reason)
	s.sessions.remove(sess)
}

// revalidateAll checks every session not checked within every.
func (s *Server) revalidateAll(now time.Time, every time.Duration) {
	if !s.checking.TryLock() {
		return
	}
	defer s.checking.Unlock()
	for _, sess := range s.sessions.all() {
		sess.mu.Lock()
		due := now.Sub(sess.checked) >= every && !sess.closed
		sess.mu.Unlock()
		if !due {
			continue
		}
		if reason := s.revalidate(sess, false); reason != "" {
			s.endSession(sess, reason)
		}
	}
}
