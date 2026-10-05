//go:build !windows

package server

import "github.com/ervisio/ervisio/server/internal/account"

// readShadow is the /etc/shadow reader.
func readShadow(name string) (*account.ShadowEntry, error) { return account.ReadShadow(name) }

// fingerprintChanged reports whether the shadow fingerprint differs.
func fingerprintChanged(old, cur string) bool { return old != cur }
