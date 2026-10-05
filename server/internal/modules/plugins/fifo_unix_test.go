//go:build unix

package plugins

import "syscall"

func mkfifo(path string, mode uint32) error { return syscall.Mkfifo(path, mode) }
