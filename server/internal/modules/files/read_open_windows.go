//go:build windows

package files

import "os"

func openReadFile(path string) (*os.File, error) { return os.Open(path) }
