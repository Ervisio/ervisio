//go:build windows

package bridge

import (
	"context"
	"errors"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// adminViaToken: on Windows admin rights are the user's elevated (UAC) token.
const adminViaToken = true

// Privileged reports whether the process token is elevated (--admin needs it).
func Privileged() bool { return windows.GetCurrentProcessToken().IsElevated() }

// RunningAsServiceIdentity reports whether the process runs as LocalSystem
// (S-1-5-18, the service identity), where --dev-insecure-noauth is refused as
// it is for root on Linux. An elevated interactive administrator is allowed:
// on Windows that is a developer running as their own user (the dev server
// does not switch users and listens on loopback only).
func RunningAsServiceIdentity() bool {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	return err != nil || u.User.Sid.IsWellKnown(windows.WinLocalSystemSid)
}

// startAdminToken is StartAdmin on Windows. The daemon (SYSTEM or an
// administrator) re-verifies the password with LogonUser, takes the user's
// elevated token (the linked token of a filtered UAC token, or the token
// itself when already full) and starts `bridge --admin` with it. Users
// outside Administrators and empty passwords (no NOPASSWD equivalent) get
// Forbidden; a wrong password gets Invalid. The token is closed when the
// bridge exits (lock, idle stop, or crash).
func startAdminToken(ctx context.Context, s *Spec, password string) (*Proc, error) {
	if password == "" {
		return nil, rpc.Errorf(rpc.Forbidden, "administrator rights on Windows need the account's password")
	}
	if len(password) > 4096 {
		return nil, rpc.Errorf(rpc.Invalid, "invalid password")
	}
	if err := pam.Authenticate(pam.Service(), s.Account.Name, password, ""); err != nil {
		switch {
		case errors.Is(err, pam.ErrAuth):
			return nil, rpc.Errorf(rpc.Invalid, "wrong password")
		case errors.Is(err, pam.ErrAccount):
			return nil, rpc.Errorf(rpc.Forbidden, "this account may not sign in")
		}
		return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
	}
	tok, err := pam.AdminToken(s.Account.Name)
	if err != nil {
		if errors.Is(err, pam.ErrNotAdmin) {
			return nil, rpc.Errorf(rpc.Forbidden, "this account is not allowed to use administrator rights")
		}
		return nil, rpc.Errorf(rpc.Unavailable, "elevated token: %v", err)
	}
	cmd := s.command(s.Bridge, s.bridgeArgs(true)...)
	if cmd.Err != nil { // no logon token / unsupported: do not elevate
		_ = tok.Close()
		return nil, rpc.Errorf(rpc.Unavailable, "%v", cmd.Err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(tok), HideWindow: true}
	p, _, err := s.start(cmd, "admin bridge "+s.Account.Name, nil)
	if err != nil {
		_ = tok.Close()
		return nil, rpc.Errorf(rpc.Unavailable, "%v", err)
	}
	go func() { // close the token once the bridge is gone
		<-p.waited
		_ = tok.Close()
	}()

	ctx, cancel := context.WithTimeout(ctx, HelloTimeout)
	defer cancel()
	helloCh := make(chan *rpc.Hello, 1)
	go func() {
		h, _ := p.Client.Hello(ctx)
		helloCh <- h
	}()
	fail := func(e *rpc.Error) (*Proc, error) {
		_ = p.cmd.Process.Kill()
		p.Stop()
		return nil, e
	}
	select {
	case h := <-helloCh:
		if h == nil {
			return fail(rpc.Errorf(rpc.Unavailable, "the admin bridge did not start"))
		}
		if !h.Admin {
			return fail(rpc.Errorf(rpc.Forbidden, "the bridge did not get administrator rights"))
		}
		p.Hello = h
		return p, nil
	case <-time.After(HelloTimeout + time.Second):
		return fail(rpc.Errorf(rpc.Unavailable, "the admin bridge did not answer in time"))
	}
}
