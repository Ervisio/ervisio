//go:build unix

package bridge

import (
	"slices"
	"testing"

	"github.com/ervisio/ervisio/server/internal/account"
)

func TestMergeEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/a", "LANG=C.UTF-8"}
	got := mergeEnv(base, []string{"XDG_RUNTIME_DIR=/run/user/1000", "LANG=it_IT.UTF-8", "PATH=/evil", "HOME=/x", "LD_PRELOAD=/x.so", "garbage", "=x"})
	want := []string{"PATH=/usr/bin", "HOME=/home/a", "LANG=it_IT.UTF-8", "XDG_RUNTIME_DIR=/run/user/1000"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestHelperCommand(t *testing.T) {
	a := &account.Account{Name: "alice", UID: 1000, GID: 1000, Home: "/home/alice", Shell: "/bin/bash"}
	s := &Spec{Bridge: "/usr/lib/ervisio/ervisio-bridge", Config: "/etc/ervisio/ervisio.conf", Account: a,
		SwitchUser: true, SessionHelper: "/usr/bin/ervisiod", PAMService: "ervisio", RHost: "192.0.2.1"}
	cmd := s.helperCommand()
	want := []string{"/usr/bin/ervisiod", HelperFlag, "--user", "alice", "--uid", "1000", "--service", "ervisio",
		"--rhost", "192.0.2.1", "--", "/usr/lib/ervisio/ervisio-bridge", "--config", "/etc/ervisio/ervisio.conf"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args %q", cmd.Args)
	}
	if cmd.SysProcAttr.Credential != nil || !cmd.SysProcAttr.Setsid || cmd.Dir != "/" {
		t.Fatal("the helper runs as root in a new session from /")
	}
}

func TestSessionHelperRefusesNonRoot(t *testing.T) {
	if RunSessionHelper([]string{"--user", "x", "--uid", "1", "--service", "login", "--", "/bin/true"}) == 0 {
		t.Fatal("helper must fail without root (or for an unknown user)")
	}
	if RunSessionHelper([]string{"--", "relative"}) == 0 {
		t.Fatal("relative bridge path accepted")
	}
}
