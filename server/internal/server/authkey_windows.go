//go:build windows

package server

import (
	"errors"
	"os"
)

const (
	oNoFollow = 0
	oNonBlock = 0
)

// checkDevAuthorizedKeys cannot check the owner and mode on Windows yet, so
// it refuses the file.
func checkDevAuthorizedKeys(f *os.File) error {
	return errors.New("--dev-authorized-keys is not supported on windows yet")
}
