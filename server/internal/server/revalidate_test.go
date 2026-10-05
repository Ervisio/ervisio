package server

import (
	"errors"
	"io"
	"log"
	"os/user"
	"runtime"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/sshauth"
	"golang.org/x/crypto/ssh"
)

type fakeAccounts struct {
	acc       *account.Account
	lookupErr error
	shadow    *account.ShadowEntry
	shadowErr error
	pamErr    error
	pamCalls  int
}

func (f *fakeAccounts) checker() *accountChecker {
	return &accountChecker{
		lookup: func(string) (*account.Account, error) {
			if f.lookupErr != nil {
				return nil, f.lookupErr
			}
			cp := *f.acc
			return &cp, nil
		},
		shadow: func(string) (*account.ShadowEntry, error) {
			if f.shadowErr != nil {
				return nil, f.shadowErr
			}
			cp := *f.shadow
			return &cp, nil
		},
		pamAcct: func(string, string) error { f.pamCalls++; return f.pamErr },
		now:     time.Now,
	}
}

func TestRevalidate(t *testing.T) {
	a, err := account.Current()
	if err != nil {
		t.Skip(err)
	}
	if !account.ShellAllowed(a.Shell) {
		t.Skipf("current user's shell %q is not a login shell", a.Shell)
	}
	base := func() (*Server, *Session, *fakeAccounts) {
		f := &fakeAccounts{acc: a, shadow: &account.ShadowEntry{Fingerprint: "fp1", Expire: -1, LastChange: 19000}}
		s := &Server{log: log.New(io.Discard, "", 0), checker: f.checker(), sessions: newStore(), cfg: testHolder(nil)}
		sess := &Session{Account: a, Created: time.Now(), lastSeen: time.Now(), shadowFP: "fp1"}
		if _, err := s.sessions.add(sess); err != nil {
			t.Fatal(err)
		}
		return s, sess, f
	}

	s, sess, f := base()
	if r := s.revalidate(sess, false); r != "" {
		t.Fatalf("unchanged account refused: %s", r)
	}
	if f.pamCalls != 0 {
		t.Fatal("periodic check with a readable shadow should not call PAM")
	}
	if r := s.revalidate(sess, true); r != "" || f.pamCalls != 1 {
		t.Fatalf("full check: %q, pam calls %d", r, f.pamCalls)
	}

	cases := []struct {
		name string
		edit func(f *fakeAccounts)
	}{
		{"removed", func(f *fakeAccounts) { f.lookupErr = user.UnknownUserError(a.Name) }},
		// Windows identifies an account by SID (the UID is only a RID).
		{"uid changed", func(f *fakeAccounts) {
			b := *f.acc
			b.UID++
			if b.SID != "" {
				b.SID += "9"
			}
			f.acc = &b
		}},
		{"nologin", func(f *fakeAccounts) { b := *f.acc; b.Shell = "/usr/sbin/nologin"; f.acc = &b }},
		{"group removed", func(f *fakeAccounts) { b := *f.acc; b.Groups, b.GroupSIDs = nil, nil; f.acc = &b }},
		{"locked", func(f *fakeAccounts) { f.shadow = &account.ShadowEntry{Fingerprint: "fp2", Locked: true, Expire: -1} }},
		{"password changed", func(f *fakeAccounts) { f.shadow = &account.ShadowEntry{Fingerprint: "fp2", Expire: -1} }},
		{"expired", func(f *fakeAccounts) { f.shadow = &account.ShadowEntry{Fingerprint: "fp1", Expire: 1} }},
		{"pam refuses (no shadow access)", func(f *fakeAccounts) {
			f.shadowErr = errors.New("open /etc/shadow: permission denied")
			f.pamErr = &pam.Error{Kind: pam.ErrAccount, Msg: "account expired"}
		}},
	}
	for _, c := range cases {
		if c.name == "nologin" && runtime.GOOS == "windows" {
			continue // accounts have no login shell on Windows (ShellAllowed is always true)
		}
		s, sess, f := base()
		c.edit(f)
		r := s.revalidate(sess, false)
		if r == "" {
			t.Errorf("%s: session kept", c.name)
			continue
		}
		// The janitor pass ends it: bridges stopped, Done closed.
		sess.checked = time.Time{}
		s.revalidateAll(time.Now(), time.Minute)
		select {
		case <-sess.Done():
		default:
			t.Errorf("%s: session not ended by revalidateAll", c.name)
		}
		if len(s.sessions.all()) != 0 {
			t.Errorf("%s: session still registered", c.name)
		}
	}

	// Errors that say nothing about the account keep the session.
	s, sess, f = base()
	f.lookupErr = errors.New("nss unavailable")
	if r := s.revalidate(sess, true); r != "" {
		t.Fatalf("transient lookup error ended the session: %s", r)
	}
	s, sess, f = base()
	f.shadowErr = errors.New("permission denied")
	f.pamErr = errors.New("pam: pam_start failed")
	if r := s.revalidate(sess, false); r != "" {
		t.Fatalf("PAM service error ended the session: %s", r)
	}
	// A recently checked session is not checked again.
	s, sess, f = base()
	sess.checked = time.Now()
	f.lookupErr = user.UnknownUserError(a.Name)
	s.revalidateAll(time.Now(), time.Minute)
	select {
	case <-sess.Done():
		t.Fatal("session checked before its interval")
	default:
	}
}

func TestAbsoluteExpiry(t *testing.T) {
	a, err := account.Current()
	if err != nil {
		t.Skip(err)
	}
	st := newStore()
	now := time.Now()
	sess := &Session{Account: a, Created: now.Add(-25 * time.Hour), lastSeen: now, expires: now.Add(-time.Minute)}
	if _, err := st.add(sess); err != nil {
		t.Fatal(err)
	}
	st.expire(now, 12*time.Hour, 5*time.Minute)
	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session past its absolute lifetime was not closed")
	}
}

// SSH-key sessions survive a locked or changed password (as with sshd),
// but not account expiry.
func TestRevalidateKeySession(t *testing.T) {
	a, err := account.Current()
	if err != nil {
		t.Skip(err)
	}
	if !account.ShellAllowed(a.Shell) {
		t.Skipf("current user's shell %q is not a login shell", a.Shell)
	}
	f := &fakeAccounts{acc: a, shadow: &account.ShadowEntry{Fingerprint: "fp2", Locked: true, Expire: -1}}
	s := &Server{log: log.New(io.Discard, "", 0), checker: f.checker(), sessions: newStore(), cfg: testHolder(nil)}
	keyOK := true
	s.checker.keyAuth = func(*account.Account, ssh.PublicKey, string) error {
		if keyOK {
			return nil
		}
		return sshauth.ErrNotAuthorized
	}
	sess := &Session{Account: a, Created: time.Now(), lastSeen: time.Now(), shadowFP: "fp1", Method: "ssh-key",
		sshKey: &sessionKey{fingerprint: "SHA256:x"}}
	if r := s.revalidate(sess, false); r != "" {
		t.Fatalf("key session ended by password lock/change: %s", r)
	}
	sess.Method, sess.sshKey = "password", nil
	if r := s.revalidate(sess, false); r == "" {
		t.Fatal("password session kept with a locked password")
	}
	sess.Method, sess.sshKey = "ssh-key", &sessionKey{fingerprint: "SHA256:x"}
	f.shadow.Expire = 1
	if r := s.revalidate(sess, false); r == "" {
		t.Fatal("expired account kept")
	}
	f.shadow.Expire = -1
	keyOK = false
	if r := s.revalidate(sess, false); r == "" {
		t.Fatal("removed key kept")
	}
}
