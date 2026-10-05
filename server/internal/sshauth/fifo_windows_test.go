package sshauth

import "errors"

func mkfifo(p string) error { return errors.New("no fifos on windows") }
