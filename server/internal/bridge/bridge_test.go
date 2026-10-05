package bridge

import (
	"context"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// exeSuffix is the executable extension of the platform.
var exeSuffix = func() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}()

func buildBridge(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "ervisio-bridge"+exeSuffix)
	cmd := exec.Command("go", "build", "-o", out, "github.com/ervisio/ervisio/server/cmd/ervisio-bridge")
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
	return &Spec{Bridge: bridge, Config: filepath.Join(t.TempDir(), "ervisio.conf"), Account: a, Logger: log.New(io.Discard, "", 0)}
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
