//go:build !windows

package config

import "os"

func readableFileACL(string, os.FileMode) error { return nil }
