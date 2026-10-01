package terminal

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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

func shSpec(script string) spec {
	return spec{Name: "t", Kind: KindLocal, Path: "/bin/sh", Argv: []string{"sh", "-c", script}, Dir: "/", Env: []string{"PATH=/usr/bin:/bin", "TERM=xterm-256color"}, Cols: 80, Rows: 24, Shell: "sh"}
}

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
	s, err := m.Create(shSpec("echo hi; sleep 0.3"))
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
	s, err := m.Create(shSpec("read x; echo got:$x; stty size; sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	_, sub := s.Subscribe()
	_ = s.Write([]byte("ping\n"))
	var out strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(out.String(), "40 100") {
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
		if _, err := m.Create(shSpec("sleep 30")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Create(shSpec("sleep 30")); !rpc.IsCode(err, rpc.Conflict) {
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
	sp, err := buildSpec(createParams{Cols: 120, Rows: 30, Cwd: "/"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if sp.Kind != KindLocal || sp.Dir != "/" || sp.Cols != 120 || sp.Rows != 30 || !strings.HasPrefix(sp.Argv[0], "-") {
		t.Fatalf("%+v", sp)
	}
	env := strings.Join(sp.Env, "\n")
	for _, want := range []string{"TERM=xterm-256color", "COLORTERM=truecolor", "LANG="} {
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

func TestParsePasswdShell(t *testing.T) {
	pw := "root:x:0:0:root:/root:/bin/sh\nbob:x:1000:1000::/home/bob:/usr/bin/nologin\nann:x:1001:1001::/home/ann:\n"
	if got := parsePasswdShell(strings.NewReader(pw), "root"); got != "/bin/sh" {
		t.Fatalf("root: %q", got)
	}
	for _, u := range []string{"bob", "ann", "nobody"} {
		if got := parsePasswdShell(strings.NewReader(pw), u); got != "" {
			t.Fatalf("%s: %q", u, got)
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
		sp := shSpec("sleep 30")
		sp.Name = got
		if _, err := m.Create(sp); err != nil {
			t.Fatal(err)
		}
	}
}
