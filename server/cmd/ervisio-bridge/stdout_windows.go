//go:build windows

package main

import "os"

// protocolStdout keeps the protocol on stdout. Windows has no dup3 to point
// fd 1 at stderr, so stray prints are not diverted yet.
func protocolStdout() (*os.File, error) { return os.Stdout, nil }
