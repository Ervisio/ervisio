//go:build windows

package server

import (
	"strconv"

	"github.com/ervisio/ervisio/server/internal/account"
)

// readShadow stands in for /etc/shadow: the fingerprint is the local
// account's password-last-set time. Domain accounts yield
// account.ErrNoShadowEntry (not checked; PAM account checks apply).
func readShadow(name string) (*account.ShadowEntry, error) {
	fp, err := account.PasswordSetFingerprint(name)
	if err != nil {
		return nil, err
	}
	return &account.ShadowEntry{Fingerprint: fp, Expire: -1, LastChange: -1}, nil
}

// fingerprintChanged compares password-last-set times (Unix seconds),
// tolerating the few seconds of skew from deriving them as now - age.
func fingerprintChanged(old, cur string) bool {
	a, e1 := strconv.ParseInt(old, 10, 64)
	b, e2 := strconv.ParseInt(cur, 10, 64)
	if e1 != nil || e2 != nil {
		return old != cur
	}
	d := a - b
	return d > 5 || d < -5
}
