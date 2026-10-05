//go:build windows

package bridge

import (
	"context"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// On Windows there is no NOPASSWD: an empty password is refused before any
// logon is attempted.
func TestAdminNeedsPasswordOnWindows(t *testing.T) {
	s := spec(t, `C:\nonexistent\ervisio-bridge.exe`)
	if _, err := StartAdmin(context.Background(), s, ""); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("got %v, want forbidden", err)
	}
}
