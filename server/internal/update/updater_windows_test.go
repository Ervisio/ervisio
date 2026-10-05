//go:build windows

package update

import (
	"context"
	"errors"
	"testing"
)

// Windows updates go through install.ps1: the self-update refuses clearly
// (the Unix tests of Apply and Switch are in updater_unix_test.go and
// switch_unix_test.go).
func TestSelfUpdateRefusedOnWindows(t *testing.T) {
	u := &Updater{Layout: testLayout(t), State: DefaultState(t.TempDir())}
	if ok, why := u.Supported("", false); ok || why != ErrWindows.Error() {
		t.Fatalf("Supported = %v, %q", ok, why)
	}
	if _, _, err := u.Apply(context.Background(), "stable", "", false, nil); !errors.Is(err, ErrWindows) {
		t.Fatalf("Apply: %v", err)
	}
	if _, _, err := u.Rollback(context.Background(), ""); !errors.Is(err, ErrWindows) {
		t.Fatalf("Rollback: %v", err)
	}
}
