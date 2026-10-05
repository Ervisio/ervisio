//go:build windows

package pam

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows sign-in: LogonUser instead of PAM. The service and rhost
// arguments are ignored (there is no PAM stack). The primary token that
// LogonUser returns is kept per user (see UserToken) so the daemon can start
// the bridge as that user with CreateProcessAsUser; it never runs a normal
// user's bridge under its own (SYSTEM) identity.

// Win32 logon errors (winerror.h), not all exported by x/sys/windows.
const (
	errNoSuchUser          = syscall.Errno(1317)
	errLogonFailure        = syscall.Errno(1326)
	errAccountRestriction  = syscall.Errno(1327)
	errInvalidLogonHours   = syscall.Errno(1328)
	errInvalidWorkstation  = syscall.Errno(1329)
	errPasswordExpired     = syscall.Errno(1330)
	errAccountDisabled     = syscall.Errno(1331)
	errLogonTypeNotGranted = syscall.Errno(1385)
	errAccountExpired      = syscall.Errno(701)
	errPasswordMustChange  = syscall.Errno(1907)
	errAccountLockedOut    = syscall.Errno(1909)
)

// splitUser splits "DOMAIN\user", "user@domain" or "user" into the name and
// domain LogonUser wants. A plain name uses "." (the local machine, then
// trusted domains); a UPN goes to LogonUser whole with a nil domain.
const (
	logon32LogonInteractive = 2
	logon32ProviderDefault  = 0
)

// x/sys/windows has no LogonUser wrapper.
var procLogonUserW = windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW")

func logonUser(name, domain, password *uint16, logonType, provider uint32, tok *windows.Token) error {
	r, _, e := procLogonUserW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(domain)),
		uintptr(unsafe.Pointer(password)), uintptr(logonType), uintptr(provider), uintptr(unsafe.Pointer(tok)))
	if r == 0 {
		return e
	}
	return nil
}

func splitUser(user string) (name, domain string, ok bool) {
	if i := strings.IndexByte(user, '\\'); i >= 0 {
		name, domain = user[i+1:], user[:i]
		return name, domain, name != "" && domain != "" && !strings.ContainsAny(name, `\@`) && !strings.Contains(domain, `\`)
	}
	if strings.Contains(user, "@") {
		i := strings.LastIndexByte(user, '@')
		return user, "", i > 0 && i < len(user)-1 // UPN: domain stays empty
	}
	return user, ".", user != ""
}

func validInput(ss ...string) bool {
	for _, s := range ss {
		if strings.ContainsRune(s, 0) {
			return false
		}
	}
	return true
}

func mapLogonError(err error) error {
	var en syscall.Errno
	if !errors.As(err, &en) {
		return fmt.Errorf("pam: LogonUser: %w", err)
	}
	switch en {
	case errLogonFailure, errNoSuchUser:
		return &Error{Kind: ErrAuth, Msg: err.Error()}
	case errAccountRestriction, errInvalidLogonHours, errInvalidWorkstation, errPasswordExpired,
		errAccountDisabled, errAccountExpired, errPasswordMustChange, errAccountLockedOut,
		errLogonTypeNotGranted:
		return &Error{Kind: ErrAccount, Msg: err.Error()}
	}
	return fmt.Errorf("pam: LogonUser: %w", err)
}

var (
	tokMu  sync.Mutex
	tokens = map[string]windows.Token{} // lower-case key -> primary token
)

func tokenKey(user string) string {
	if i := strings.IndexByte(user, '\\'); i >= 0 {
		user = user[i+1:]
	}
	if i := strings.IndexByte(user, '@'); i > 0 {
		user = user[:i]
	}
	return strings.ToLower(user)
}

// Authenticate checks user/password with LogonUser (interactive logon, so
// the token is a full one). On success a primary token is kept for
// UserToken. The password's UTF-16 copy is wiped after use.
func Authenticate(service, user, password, rhost string) error {
	if user == "" || !validInput(user, password, rhost) {
		return &Error{Kind: ErrAuth, Msg: "invalid input"}
	}
	name, domain, ok := splitUser(user)
	if !ok {
		return &Error{Kind: ErrAuth, Msg: "invalid user name"}
	}
	pw, err := windows.UTF16FromString(password)
	if err != nil {
		return &Error{Kind: ErrAuth, Msg: "invalid input"}
	}
	defer func() {
		for i := range pw {
			pw[i] = 0
		}
	}()
	pName, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return &Error{Kind: ErrAuth, Msg: "invalid input"}
	}
	var pDomain *uint16
	if domain != "" {
		if pDomain, err = windows.UTF16PtrFromString(domain); err != nil {
			return &Error{Kind: ErrAuth, Msg: "invalid input"}
		}
	}
	var tok windows.Token
	if err := logonUser(pName, pDomain, &pw[0], logon32LogonInteractive, logon32ProviderDefault, &tok); err != nil {
		return mapLogonError(err)
	}
	// LogonUser returns a primary token; duplicate explicitly so the kept
	// handle is guaranteed primary (needed by CreateProcessAsUser).
	var primary windows.Token
	err = windows.DuplicateTokenEx(tok, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &primary)
	_ = tok.Close()
	if err != nil {
		return fmt.Errorf("pam: DuplicateTokenEx: %w", err)
	}
	key := tokenKey(user)
	tokMu.Lock()
	defer tokMu.Unlock()
	if _, have := tokens[key]; have {
		// Keep the first token: a bridge may be about to start with it, so it
		// is never closed behind its back. Group changes need Forget + login.
		_ = primary.Close()
		return nil
	}
	tokens[key] = primary
	return nil
}

// UserToken returns the primary token kept from the user's last successful
// Authenticate, for syscall.SysProcAttr.Token. The handle is owned by this
// package: callers must not close it.
func UserToken(user string) (syscall.Token, bool) {
	tokMu.Lock()
	defer tokMu.Unlock()
	t, ok := tokens[tokenKey(user)]
	return syscall.Token(t), ok
}

func lookupToken(user string) (windows.Token, bool) {
	tokMu.Lock()
	defer tokMu.Unlock()
	t, ok := tokens[tokenKey(user)]
	return t, ok
}

// Forget closes and drops the kept token of user (e.g. when the account is
// refused on re-validation).
func Forget(user string) {
	tokMu.Lock()
	defer tokMu.Unlock()
	k := tokenKey(user)
	if t, ok := tokens[k]; ok {
		_ = t.Close()
		delete(tokens, k)
	}
}

// CheckAccount is best effort: the account must exist (LookupAccountName).
// Disabled or expired accounts are only detected at the next logon.
func CheckAccount(service, user, rhost string) error {
	if user == "" || !validInput(user, rhost) {
		return &Error{Kind: ErrAccount, Msg: "invalid input"}
	}
	name, domain, ok := splitUser(user)
	if !ok {
		return &Error{Kind: ErrAccount, Msg: "invalid user name"}
	}
	full := name
	if domain != "" && domain != "." {
		full = domain + `\` + name
	}
	sid, _, _, err := windows.LookupSID("", full)
	if err != nil || sid == nil {
		return &Error{Kind: ErrAccount, Msg: "unknown account"}
	}
	return nil
}

// Session is empty on Windows: there is no PAM session to hold.
type Session struct{}

// OpenSession validates the user name and returns an empty Session.
func OpenSession(service, user, rhost string) (*Session, error) {
	if user == "" || !validInput(user, rhost) {
		return nil, &Error{Kind: ErrAccount, Msg: "invalid input"}
	}
	return &Session{}, nil
}

// Env returns nil.
func (s *Session) Env() []string { return nil }

// Close does nothing.
func (s *Session) Close() error { return nil }
