package files

import (
	"io"
	"os"
)

func openSearchTextFile(root *os.Root, name string) (io.ReadCloser, error) {
	f, err := root.Open(name)
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
