package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultsWhenMissing(t *testing.T) {
	cfg, exists, _, err := Load(filepath.Join(t.TempDir(), "nope.conf"))
	if err != nil || exists {
		t.Fatalf("err=%v exists=%v", err, exists)
	}
	if cfg.Listen != "0.0.0.0:9090" || cfg.Session.AdminUnlock.Duration != 5*time.Minute || !cfg.Login.ShowIP {
		t.Fatalf("bad defaults: %+v", cfg)
	}
}

func TestLoadPartialAndUnknown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	os.WriteFile(p, []byte("allow_root = true\nbogus = 1\n[session]\ntimeout = \"1h\"\n"), 0o644)
	cfg, exists, warn, err := Load(p)
	if err != nil || !exists {
		t.Fatal(err)
	}
	if !cfg.AllowRoot || cfg.Session.Timeout.Duration != time.Hour || cfg.Login.MaxFailures != 5 {
		t.Fatalf("got %+v", cfg)
	}
	if len(warn) != 1 || !strings.Contains(warn[0], "bogus") {
		t.Fatalf("warnings %v", warn)
	}
}

func TestLoadInvalid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	os.WriteFile(p, []byte("[tls]\nmode = \"weird\"\n"), 0o644)
	if _, _, _, err := Load(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveBackupRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "c.conf")
	cfg := Default()
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Set("login.max_failures", float64(9)); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Set("session.admin_unlock", "10m"); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	bak, _ := os.ReadFile(p + ".bak")
	if !strings.Contains(string(bak), "max_failures = 5") {
		t.Fatalf("backup content: %s", bak)
	}
	got, _, _, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Login.MaxFailures != 9 || got.Session.AdminUnlock.Duration != 10*time.Minute {
		t.Fatalf("round trip: %+v", got)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), `admin_unlock = "10m"`) {
		t.Fatalf("duration format: %s", data)
	}
}

func TestSetValidation(t *testing.T) {
	cfg := Default()
	bad := map[string]any{
		"nope":                 true,
		"allow_root":           "yes",
		"login.max_failures":   1.5,
		"session.timeout":      "1s",
		"listen":               "nonsense",
		"tls.mode":             "plain",
		"tls.cert":             "relative/path",
		"session.admin_unlock": 5,
	}
	for k, v := range bad {
		if err := cfg.Set(k, v); err == nil {
			t.Errorf("%s=%v accepted", k, v)
		}
	}
	if err := cfg.Set("listen", "127.0.0.1:9191"); err != nil {
		t.Fatal(err)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{12 * time.Hour: "12h", 5 * time.Minute: "5m", 90 * time.Second: "1m30s", 30 * time.Second: "30s", time.Hour + 30*time.Minute: "1h30m"} {
		if got := formatDuration(d); got != want {
			t.Errorf("%v: got %q want %q", d, got, want)
		}
	}
}

func TestDiff(t *testing.T) {
	d := Diff("a\nb\nc\n", "a\nx\nc\n")
	if d != " a\n-b\n+x\n c\n" {
		t.Fatalf("diff %q", d)
	}
}

func TestUpdatesKeys(t *testing.T) {
	cfg := Default()
	if cfg.Updates.Channel != "stable" || !cfg.Updates.AutoCheck || cfg.Updates.AutoInstall || cfg.Updates.AutoInstallAt != "03:30" {
		t.Fatalf("bad update defaults: %+v", cfg.Updates)
	}
	good := map[string]any{"updates.channel": "prerelease", "updates.auto_check": false, "updates.auto_install": true, "updates.auto_install_at": "23:59"}
	for k, v := range good {
		if err := cfg.Set(k, v); err != nil {
			t.Errorf("%s=%v: %v", k, v, err)
		}
	}
	bad := map[string][]any{
		"updates.channel":         {"nightly", "", true},
		"updates.auto_check":      {"yes", 1.0},
		"updates.auto_install_at": {"24:00", "3:30", "03:60", "03-30", "+3:30", "03:3x", " 3:30", "", 330.0},
	}
	for k, vs := range bad {
		for _, v := range vs {
			if err := cfg.Set(k, v); err == nil {
				t.Errorf("%s=%#v accepted", k, v)
			}
		}
	}
	if m, err := ParseClock("03:30"); err != nil || m != 210 {
		t.Fatalf("ParseClock = %d, %v", m, err)
	}
	p := filepath.Join(t.TempDir(), "c.conf")
	os.WriteFile(p, []byte("[updates]\nchannel = \"beta\"\n"), 0o644)
	if _, _, _, err := Load(p); err == nil {
		t.Fatal("invalid channel in file accepted")
	}
	os.WriteFile(p, []byte("[updates]\nauto_install = true\nauto_install_at = \"04:15\"\n"), 0o644)
	c, _, _, err := Load(p)
	if err != nil || !c.Updates.AutoInstall || c.Updates.AutoInstallAt != "04:15" || c.Updates.Channel != "stable" || !c.Updates.AutoCheck {
		t.Fatalf("load updates: %+v %v", c.Updates, err)
	}
}

func TestAuthAllowlistKeys(t *testing.T) {
	c := Default()
	if c.Auth.Restricted() {
		t.Fatal("default must allow everyone")
	}
	if v := c.Values()["auth.allow_users"]; v == nil {
		t.Fatal("empty list must be [] in JSON, not null")
	}
	if err := c.Set("auth.allow_users", []any{"alice", " bob "}); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("auth.allow_groups", []any{"wheel", "Domain_Users@corp"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("auth.admins_only", true); err != nil {
		t.Fatal(err)
	}
	if got := c.Auth.AllowUsers; len(got) != 2 || got[1] != "bob" || !c.Auth.Restricted() {
		t.Fatalf("got %+v", c.Auth)
	}
	for _, bad := range []any{[]any{"a b"}, []any{"-x"}, []any{""}, []any{"a;rm"}, []any{"x", "x"}, []any{1.0}, "alice", []any{strings.Repeat("a", 70)}} {
		if err := c.Set("auth.allow_users", bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	// Round trip through the file.
	p := filepath.Join(t.TempDir(), "c.conf")
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := Load(p)
	if err != nil || len(got.Auth.AllowUsers) != 2 || got.Auth.AllowGroups[0] != "wheel" || !got.Auth.AdminsOnly {
		t.Fatalf("%v %+v", err, got)
	}
	cl := got.Clone()
	cl.Auth.AllowUsers[0] = "zed"
	if got.Auth.AllowUsers[0] != "alice" {
		t.Fatal("Clone shares the allow_users slice")
	}
}

func TestPlainHTTPMode(t *testing.T) {
	ok := []string{"127.0.0.1:9090", "127.5.5.5:80", "[::1]:9090"}
	bad := []string{"0.0.0.0:9090", ":9090", "192.168.1.5:9090", "[::]:9090", "[2001:db8::1]:9090", "localhost:9090"}
	for _, l := range ok {
		c := Default()
		c.Listen, c.TLS.Mode = l, "http"
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", l, err)
		}
	}
	for _, l := range bad {
		c := Default()
		c.Listen, c.TLS.Mode = l, "http"
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Errorf("%s accepted or unclear error: %v", l, err)
		}
	}
	// Other modes do not care.
	c := Default()
	c.Listen = "0.0.0.0:9090"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	// Loading a file applies the same rule.
	p := filepath.Join(t.TempDir(), "c.conf")
	os.WriteFile(p, []byte("[tls]\nmode = \"http\"\n"), 0o644)
	if _, _, _, err := Load(p); err == nil {
		t.Fatal("default listen 0.0.0.0 with plain http must be rejected")
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "c.conf")
		os.WriteFile(p, []byte(s), 0o644)
		return p
	}
	if _, err := Check(filepath.Join(dir, "missing.conf")); err == nil {
		t.Fatal("missing file must fail")
	}
	if w, err := Check(write("listen = \"127.0.0.1:9090\"\nbogus = 1\n[tls]\nmode = \"http\"\n")); err != nil || len(w) != 1 {
		t.Fatalf("%v %v", w, err)
	}
	if _, err := Check(write("[tls]\nmode = \"custom\"\n")); err == nil {
		t.Fatal("custom without cert must fail")
	}
	if _, err := Check(write("[tls]\nmode = \"custom\"\ncert = \"/nonexistent/a.crt\"\nkey = \"/nonexistent/a.key\"\n")); err == nil {
		t.Fatal("custom with missing files must fail")
	}
	if _, err := Check(write("listen = [")); err == nil {
		t.Fatal("syntax error must fail")
	}
}
