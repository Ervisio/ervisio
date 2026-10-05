//go:build windows

package sys

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Windows counterparts of exec_unix_test.go, with cmd.exe and ping.

func TestRunOutputWindows(t *testing.T) {
	ctx := context.Background()
	out, err := Output(ctx, "cmd", "/c", "echo", "a b")
	if err != nil || strings.TrimSpace(string(out)) != "a b" {
		t.Fatalf("%q %v", out, err)
	}
	err = Run(ctx, "cmd", "/c", "echo boom 1>&2& exit 3")
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 3 || !strings.Contains(ee.Error(), "boom") {
		t.Fatalf("%v", err)
	}
	if err := Run(ctx, "definitely-not-a-command-xyz"); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := (Cmd{Name: "cmd", Args: []string{"/c", "echo", "0123456789012345"}, MaxOutput: 10}).Output(ctx); err == nil {
		t.Fatal("expected overflow error")
	}
	if err := (Cmd{Name: "ping", Args: []string{"-n", "6", "127.0.0.1"}, Timeout: 100 * time.Millisecond}).Run(ctx); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("timeout: %v", err)
	}
	if out, _ := (Cmd{Name: "cmd", Args: []string{"/c", "echo", "%PATH%", "%FOO%"}, Env: []string{"FOO=bar"}}).Output(ctx); strings.TrimSpace(string(out)) != SafePath+" bar" {
		t.Fatalf("env: %q", out)
	}
}

func TestLookPathWindows(t *testing.T) {
	p, err := LookPath("cmd")
	if err != nil || !strings.HasSuffix(strings.ToLower(p), `\cmd.exe`) {
		t.Fatalf("cmd: %q %v", p, err)
	}
	if q, err := LookPath(p); err != nil || q != p {
		t.Fatalf("absolute: %q %v", q, err)
	}
	if _, err := LookPath(`relative\cmd.exe`); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatalf("relative path: %v", err)
	}
}

func TestStreamWindows(t *testing.T) {
	var lines []string
	err := Stream(context.Background(), "cmd", []string{"/c", "echo a& echo b& echo c"}, func(l string) error {
		lines = append(lines, l)
		return nil
	})
	if err != nil || strings.Join(lines, ",") != "a,b,c" {
		t.Fatalf("%v %v", lines, err)
	}
	stop := errors.New("stop")
	err = Stream(context.Background(), "cmd", []string{"/c", "echo a& echo b"}, func(string) error { return stop })
	if err != stop {
		t.Fatalf("%v", err)
	}
}
