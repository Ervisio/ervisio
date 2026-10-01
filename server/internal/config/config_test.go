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
