//go:build unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// protocolStdout moves the protocol to a private duplicate of stdout and
// points fd 1 at stderr, so stray prints from modules or libraries cannot
// corrupt the JSON stream.
func protocolStdout() (*os.File, error) {
	fd, err := syscall.Dup(1)
	if err != nil {
		return nil, fmt.Errorf("dup stdout: %w", err)
	}
	syscall.CloseOnExec(fd)
	if err := syscall.Dup3(2, 1, 0); err != nil {
		return nil, fmt.Errorf("redirect stdout: %w", err)
	}
	return os.NewFile(uintptr(fd), "protocol"), nil
}
