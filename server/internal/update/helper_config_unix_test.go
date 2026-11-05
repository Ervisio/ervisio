//go:build !windows

package update

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUpdateAndRollbackKeepActiveConfigPath(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.u.ConfigPath = filepath.Join(t.TempDir(), "custom config.conf")
	h.gh.publish("1.1.0", h.sk, nil)
	if _, _, err := h.u.Apply(context.Background(), ChannelStable, "1.1.0", true, func(Event) {}); err != nil {
		t.Fatal(err)
	}
	target, kind, path, auto, ok := parseHelperArgs(h.launched[0][2:], func(string, ...any) {})
	if !ok || target != "1.1.0" || kind != KindUpdate || path != h.u.ConfigPath || !auto {
		t.Fatalf("update args: %v", h.launched[0])
	}
	addVersion(t, h.u.Layout, "0.9.0")
	if err := h.u.Layout.SetPrevious("0.9.0"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.u.Rollback(context.Background(), "0.9.0"); err != nil {
		t.Fatal(err)
	}
	target, kind, path, _, ok = parseHelperArgs(h.launched[1][2:], func(string, ...any) {})
	if !ok || target != "0.9.0" || kind != KindRollback || path != h.u.ConfigPath {
		t.Fatalf("rollback args: %v", h.launched[1])
	}
}
