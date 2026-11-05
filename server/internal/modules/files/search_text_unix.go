//go:build unix

package files

import (
	"io"
	"os"
	"syscall"
)

func openSearchTextFile(root *os.Root, name string) (io.ReadCloser, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		return nil, errNotRegularText()
	}
	return f, nil
}
