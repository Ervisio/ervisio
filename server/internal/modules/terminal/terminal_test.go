package terminal

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func TestRingKeepsLatest(t *testing.T) {
	r := newRing(8)
	r.Write([]byte("abc"))
	if got := string(r.Snapshot()); got != "abc" {
		t.Fatalf("got %q", got)
	}
	r.Write([]byte("defgh"))
	if got := string(r.Snapshot()); got != "abcdefgh" {
		t.Fatalf("exactly full: got %q", got)
	}
	r.Write([]byte("ij"))
	if got := string(r.Snapshot()); got != "cdefghij" {
		t.Fatalf("wrapped: got %q", got)
	}
	r.Write([]byte("0123456789ABCDEF"))
	if got := string(r.Snapshot()); got != "89ABCDEF" {
		t.Fatalf("oversized write: got %q", got)
	}
}

func TestRingScrollbackSize(t *testing.T) {
	r := newRing(ScrollbackSize)
	chunk := bytes.Repeat([]byte("x"), 100000)
	for i := 0; i < 5; i++ {
		r.Write(chunk)
	}
	if n := len(r.Snapshot()); n != ScrollbackSize {
		t.Fatalf("len %d, want %d", n, ScrollbackSize)
	}
}

// sleepCmd keeps a session alive for about 30 seconds.
func sleepCmd() (unix, windows string) { return "sleep 30", "ping -n 31 127.0.0.1 >nul" }

// shSpec is a session running a script through the platform's shell: unix is
// the sh -c script, windows the cmd /c one (Windows has neither sh nor sleep).
func shSpec(unix, windows string) spec {
	if runtime.GOOS == "windows" {
		cmdExe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "cmd.exe")
		return spec{Name: "t", Kind: KindLocal, Path: cmdExe, Argv: []string{"cmd", "/v:on", "/c", windows}, Dir: os.TempDir(), Cols: 80, Rows: 24, Shell: "cmd"}
	}
	return spec{Name: "t", Kind: KindLocal, Path: "/bin/sh", Argv: []string{"sh", "-c", unix}, Dir: "/", Env: []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color"}, Cols: 80, Rows: 24, Shell: "sh"}
}

// sleepSpec is a session that just stays alive.
func sleepSpec() spec { return shSpec(sleepCmd()) }

func collect(t *testing.T, s *Session, sub *subscriber, replay []byte) string {
	t.Helper()
	var out bytes.Buffer
	out.Write(replay)
	for {
		select {
		case b := <-sub.ch:
			out.Write(b)
		case <-s.Done():
			for {
				select {
				case b := <-sub.ch:
					out.Write(b)
				default:
					return out.String()
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for the session")
		}
	}
}

func TestSessionRunsAndReplays(t *testing.T) {
	m := newManager()
	s, err := m.Create(shSpec("echo hi; sleep 0.3", "echo hi& ping -n 2 127.0.0.1 >nul"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	// a late attach gets the earlier output as replay
	replay, sub := s.Subscribe()
	if !strings.Contains(string(replay), "hi") {
		t.Fatalf("replay %q lacks output", replay)
	}
	if !s.Info().Attached {
		t.Fatal("expected attached")
	}
	out := collect(t, s, sub, replay)
	if !strings.Contains(out, "hi") {
		t.Fatalf("output %q", out)
	}
	if s.ExitCode() != 0 {
		t.Fatalf("exit %d", s.ExitCode())
	}
	if len(m.List()) != 0 {
		t.Fatal("ended session should be gone from the list")
	}
	if _, err := m.Get(s.id); !rpc.IsCode(err, rpc.NotFound) {
		t.Fatalf("want not_found, got %v", err)
	}
}

func TestSessionInputResizeKill(t *testing.T) {
	m := newManager()
	defer m.CloseAll()
	// The program reads a line, echoes it and prints the terminal size
	// (stty size: "40 100"; on Windows mode con: "Lines: 40", "Columns: 100").
	s, err := m.Create(shSpec("read x; echo got:$x; stty size; sleep 30", "set /p x=& echo got:!x!& mode con& ping -n 31 127.0.0.1 >nul"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	_, sub := s.Subscribe()
	enter, sized := "\n", func(o string) bool { return strings.Contains(o, "40 100") }
	if runtime.GOOS == "windows" {
		enter = "\r" // a console takes Enter as CR
		sized = func(o string) bool {
			return strings.Contains(o, "Lines:") && strings.Contains(o, "40") && strings.Contains(o, "100")
		}
	}
	_ = s.Write([]byte("ping" + enter))
	var out strings.Builder
	deadline := time.After(5 * time.Second)
	for !sized(out.String()) {
		select {
		case b := <-sub.ch:
			out.Write(b)
		case <-deadline:
			t.Fatalf("output %q", out.String())
		}
	}
	if !strings.Contains(out.String(), "got:ping") {
		t.Fatalf("input not echoed by the program: %q", out.String())
	}
	s.Rename("renamed")
	if got := m.List()[0]; got.Name != "renamed" || got.Kind != KindLocal || !got.Attached {
		t.Fatalf("info %+v", got)
	}
	s.Kill(time.Second)
	select {
	case <-s.Done():
	default:
		t.Fatal("session still running after kill")
	}
}

func TestSessionLimit(t *testing.T) {
	m := newManager()
	defer m.CloseAll()
	for i := 0; i < MaxSessions; i++ {
		if _, err := m.Create(sleepSpec()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Create(sleepSpec()); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestOSCTitle(t *testing.T) {
	var o oscState
	if _, ok := o.feed([]byte("hello")); ok {
		t.Fatal("no title expected")
	}
	title, ok := o.feed([]byte("\x1b]0;user@host: ~/src\x07prompt$ "))
	if !ok || title != "user@host: ~/src" {
		t.Fatalf("got %q %v", title, ok)
	}
	title, ok = o.feed([]byte("\x1b]2;vim\x1b\\"))
	if !ok || title != "vim" {
		t.Fatalf("got %q %v", title, ok)
	}
}

func TestBuildSpecValidation(t *testing.T) {
	bad := []createParams{
		{Kind: "ssh", Host: ""},
		{Kind: "ssh", Host: "-oProxyCommand=evil"},
		{Kind: "ssh", Host: "a b"},
		{Kind: "ssh", Host: "host;rm"},
		{Kind: "ssh", Host: "ok.example.com", User: "-x"},
		{Kind: "ssh", Host: "ok.example.com", User: "a b"},
		{Kind: "ssh", Host: "ok.example.com", Port: 70000},
		{Kind: "nope"},
		{Cols: 5000, Rows: 10},
		{Shell: "bash"},
		{Shell: "/tmp/not-a-shell"},
		{Cwd: "relative"},
		{Cwd: "/definitely/not/here"},
		{Name: "bad\x01name"},
		{Kind: "root"},
	}
	for _, p := range bad {
		if _, err := buildSpec(p, false); err == nil {
			t.Errorf("expected an error for %+v", p)
		}
	}
}

func TestBuildSpecLocalAndSSH(t *testing.T) {
	cwd, loginShell := "/", true // Windows has no "/" folder, and its shells are not login shells (argv[0] "-")
	if runtime.GOOS == "windows" {
		cwd, loginShell = os.TempDir(), false
	}
	sp, err := buildSpec(createParams{Cols: 120, Rows: 30, Cwd: cwd}, false)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Kind != KindLocal || sp.Dir != cwd || sp.Cols != 120 || sp.Rows != 30 || (loginShell && !strings.HasPrefix(sp.Argv[0], "-")) {
		t.Fatalf("%+v", sp)
	}
	env := strings.Join(sp.Env, "\n")
	wants := []string{"TERM=xterm-256color", "COLORTERM=truecolor", "LANG="}
	if runtime.GOOS == "windows" {
		wants = wants[:2] // no LANG on Windows
	}
	for _, want := range wants {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s", want)
		}
	}
	root, err := buildSpec(createParams{}, true)
	if err != nil || root.Kind != KindRoot {
		t.Fatalf("admin spec: %+v %v", root, err)
	}
	if _, err := os.Stat("/usr/bin/ssh"); err == nil {
		sp, err := buildSpec(createParams{Kind: "ssh", Host: "example.com", User: "bob", Port: 2222}, false)
		if err != nil {
			t.Fatal(err)
		}
		want := "ssh -o ServerAliveInterval=30 -p 2222 -l bob -- example.com"
		if got := strings.Join(sp.Argv, " "); got != want || sp.Kind != KindSSH {
			t.Fatalf("argv %q", got)
		}
	}
}

func TestUniqueName(t *testing.T) {
	m := newManager()
	defer m.CloseAll()
	for i, want := range []string{"a", "a 2", "a 3"} {
		got := m.uniqueName("a")
		if got != want {
			t.Fatalf("#%d: got %q want %q", i, got, want)
		}
		sp := sleepSpec()
		sp.Name = got
		if _, err := m.Create(sp); err != nil {
			t.Fatal(err)
		}
	}
}
