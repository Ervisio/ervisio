package files

// Platform-neutral helpers of the safe file system layer. The directory
// descriptor primitives live in safefs_unix.go; safefs_windows.go refuses
// them until a Windows implementation exists.

import (
	"errors"
	"os"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func pathErr(op, path string, err error) error {
	if err == nil {
		return nil
	}
	var pe *os.PathError
	var re *rpc.Error
	if errors.As(err, &pe) || errors.As(err, &re) {
		return err
	}
	return &os.PathError{Op: op, Path: path, Err: err}
}

func errUntrustedLink(path string) error {
	return rpc.Errorf(rpc.Forbidden, "%s goes through a link that belongs to another user. With administrator rights, open the folder the link points to instead.", path)
}
