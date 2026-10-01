package bridge

import (
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

func buildBridge(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "linuxadmin-bridge")
	cmd := exec.Command("go", "build", "-o", out, "github.com/Fonlogen/LinuxAdmin/server/cmd/linuxadmin-bridge")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build bridge: %v\n%s", err, b)
	}
	return out
}

func spec(t *testing.T, bridge string) *Spec {
	a, err := account.Current()
	if err != nil {
		t.Fatal(err)
	}
	return &Spec{Bridge: bridge, Config: "/nonexistent/linuxadmin.conf", Account: a, Logger: log.New(io.Discard, "", 0)}
}

// fakeSudo writes a shell script standing in for sudo.
func fakeSudo(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "sudo")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUserBridge(t *testing.T) {
	s := spec(t, buildBridge(t))
	p, err := StartUser(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if p.Hello.Admin || p.Hello.Methods["system.host"] != "user" {
		t.Fatalf("hello %+v", p.Hello)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Call(ctx, "system.host", nil); err != nil {
		t.Fatal(err)
	}
	p.Stop()
	select {
	case <-p.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not exit")
	}
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

func TestClassifySudo(t *testing.T) {
	if classifySudo("sudo: 3 incorrect password attempts") != rpc.Invalid ||
		classifySudo("alice is not in the sudoers file.  This incident will be reported.") != rpc.Forbidden ||
		classifySudo("We trust you have received the usual lecture") != "" {
		t.Fatal("classification")
	}
}

func TestHold(t *testing.T) {
	p := &Proc{}
	if p.Busy() {
		t.Fatal("new proc busy")
	}
	r1, r2 := p.Hold(), p.Hold()
	r1()
	r1() // idempotent
	if !p.Busy() {
		t.Fatal("one hold left")
	}
	r2()
	if p.Busy() {
		t.Fatal("released")
	}
}
