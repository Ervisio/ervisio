//go:build windows

package sshauth

import "errors"

var errUnsupported = errors.New("not supported on windows")

// readSecure cannot check ownership and modes like sshd does on Windows, so
// it refuses to read.
func readSecure(p string, u User) ([]byte, error) { return nil, errUnsupported }

// switchFS: there is no file-system identity to switch on Windows.
func switchFS(u User) (restore func() error, err error) {
	return func() error { return nil }, errUnsupported
}
