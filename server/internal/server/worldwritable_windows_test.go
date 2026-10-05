//go:build windows

package server

import (
	"testing"

	"github.com/ervisio/ervisio/server/internal/winsec"
)

// makeWorldWritable lets Everyone write to the file at path (a protected
// DACL of the daemon's user and Everyone; Windows has no POSIX modes).
func makeWorldWritable(t *testing.T, path string) {
	t.Helper()
	me, err := winsec.ProcessUser()
	if err != nil {
		t.Fatal(err)
	}
	err = winsec.SetProtectedDACL(path, []winsec.Entry{
		{SID: me, Mask: winsec.FileAllAccess},
		{SID: winsec.Everyone(), Mask: winsec.FileAllAccess},
	})
	if err != nil {
		t.Fatal(err)
	}
}
