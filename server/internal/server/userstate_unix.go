//go:build !windows

package server

import (
	"os"
	"syscall"
)

func createStateDirectory(path string) error { return os.Mkdir(path, 0700) }

func stateOwnedBySelf(_ string, info os.FileInfo) bool {
	value, ok := info.Sys().(*syscall.Stat_t)
	return ok && value.Uid == uint32(os.Geteuid())
}
