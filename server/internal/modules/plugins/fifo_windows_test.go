//go:build windows

package plugins

import "errors"

func mkfifo(path string, mode uint32) error { return errors.New("no fifos on windows") }
