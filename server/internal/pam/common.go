package pam

import (
	"errors"
	"os"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Errors returned by Authenticate.
var (
	// ErrAuth means wrong user name or password.
	ErrAuth = errors.New("authentication failed")
	// ErrAccount means the password was accepted but the account may not
	// sign in (expired, locked, password change required…).
	ErrAccount = errors.New("account not available")
)

// Error carries PAM's own description.
type Error struct {
	Kind error
	Msg  string
}

func (e *Error) Error() string { return e.Kind.Error() + ": " + e.Msg }
func (e *Error) Unwrap() error { return e.Kind }

// Service returns the PAM service to use: "ervisio" when
// /etc/pam.d/ervisio exists, otherwise "login".
func Service() string {
	if _, err := os.Stat("/etc/pam.d/" + brand.PAMService); err == nil {
		return brand.PAMService
	}
	return brand.PAMFallbackService
}
