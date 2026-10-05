//go:build !(linux && cgo) && !windows

package pam

import "errors"

// errUnsupported is returned on platforms without libpam (a
// build with cgo disabled). Sign-in there needs its own backend.
var errUnsupported = errors.New("pam: not supported on this platform")

// Authenticate always fails without libpam.
func Authenticate(service, user, password, rhost string) error {
	return &Error{Kind: ErrAuth, Msg: errUnsupported.Error()}
}

// CheckAccount always fails without libpam.
func CheckAccount(service, user, rhost string) error {
	return &Error{Kind: ErrAccount, Msg: errUnsupported.Error()}
}

// Session is a PAM session; it cannot be opened without libpam.
type Session struct{}

// OpenSession always fails without libpam.
func OpenSession(service, user, rhost string) (*Session, error) {
	return nil, &Error{Kind: ErrAccount, Msg: errUnsupported.Error()}
}

// Env returns nil.
func (s *Session) Env() []string { return nil }

// Close does nothing.
func (s *Session) Close() error { return nil }
