//go:build !windows

package server

import (
	"os"
	"testing"
)

// makeWorldWritable lets everyone write to the file at path.
func makeWorldWritable(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
}
