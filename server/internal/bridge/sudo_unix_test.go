//go:build !windows

package bridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Administrator rights come from sudo on Unix; these tests use a fake sudo script.

// fakeSudo writes a shell script standing in for sudo.
func fakeSudo(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "sudo")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAdminSudoOutcomes(t *testing.T) {
	bridge := buildBridge(t)
	cases := []struct {
		name string
		sudo string
		want rpc.Code
	}{
		{"wrong password", `read pw; echo "Sorry, try again." >&2; read pw2; exit 1`, rpc.Invalid},
		{"not in sudoers", `read pw; echo "bob is not in the sudoers file." >&2; exit 1`, rpc.Forbidden},
		{"no root", `read pw; shift 5; exec "$1" --config /x/y.conf`, rpc.Forbidden}, // bridge without --admin → not root
		{"silent failure", `read pw; exit 1`, rpc.Unavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := spec(t, bridge)
			s.Sudo = fakeSudo(t, c.sudo)
			start := time.Now()
			_, err := StartAdmin(context.Background(), s, "secret")
			if !rpc.IsCode(err, c.want) {
				t.Fatalf("got %v, want %s", err, c.want)
			}
			if time.Since(start) > 5*time.Second {
				t.Fatalf("took %v", time.Since(start))
			}
		})
	}
	s := spec(t, bridge)
	if _, err := StartAdmin(context.Background(), s, "two\nlines"); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
}

// An empty password runs sudo -n (NOPASSWD only) and writes nothing to
// sudo's stdin.
func TestAdminNoPassword(t *testing.T) {
	bridge := buildBridge(t)
	s := spec(t, bridge)
	s.Sudo = fakeSudo(t, `[ "$1" = "-n" ] && [ "$2" = "-k" ] && [ "$3" = "--" ] || { echo "bad args: $*" >&2; exit 1; }; echo "sudo: a password is required" >&2; exit 1`)
	if _, err := StartAdmin(context.Background(), s, ""); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("got %v, want invalid (password required)", err)
	}
	// NOPASSWD: sudo runs the bridge directly; here it is not root, so the
	// hello check refuses it, which proves the bridge was started.
	s.Sudo = fakeSudo(t, `[ "$1" = "-n" ] || exit 1; shift 3; exec "$1" --config /x/y.conf`)
	if _, err := StartAdmin(context.Background(), s, ""); !rpc.IsCode(err, rpc.Forbidden) {
		t.Fatalf("got %v, want forbidden (not root)", err)
	}
}
