//go:build !windows

package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/update"
)

// put writes files under root: path -> content (mode 0644, or 0600 when
// the path contains "key").
func put(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.Contains(p, "key") {
			mode = 0o600
		}
		if err := os.WriteFile(full, []byte(c), mode); err != nil {
			t.Fatal(err)
		}
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const legacyConf = `# LinuxAdmin server configuration, written by the installer on 2026-10-01.
# Settings that say "restart" need: systemctl restart linuxadmin. Saving from
# Settings rewrites this file (a backup stays next to it as linuxadmin.conf.bak).
listen = "0.0.0.0:9443"

[tls]
mode = "custom"
cert = "/etc/linuxadmin/certs/site.pem"
key = "/etc/linuxadmin/certs/site.key"
`

const legacyPAM = `#%PAM-1.0
# PAM service for LinuxAdmin sign-in (/etc/pam.d/linuxadmin): Arch Linux and
# derivatives. The session stack runs around every user bridge (pam_limits,
# pam_loginuid, pam_systemd...), opened by linuxadmind's root session helper.
auth      include   system-login
account   include   system-login
session   include   system-login
`

// legacySystem lays out a LinuxAdmin 0.2.0 installation under root.
func legacySystem(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	put(t, root, map[string]string{
		"/etc/linuxadmin/linuxadmin.conf":                             legacyConf,
		"/etc/linuxadmin/linuxadmin.conf.bak":                         "listen = \"0.0.0.0:9090\"\n",
		"/etc/linuxadmin/tls/self-signed.crt":                         "CERT",
		"/etc/linuxadmin/tls/self-signed.key":                         "KEY",
		"/etc/linuxadmin/certs/site.pem":                              "PEM",
		"/etc/linuxadmin/plugins-catalog.url":                         "https://example.org/catalog.json\n",
		"/var/lib/linuxadmin/plugins/docker/manifest.json":            "{}",
		"/var/lib/linuxadmin/plugins-state.json":                      `{"docker":{"enabled":true}}`,
		"/var/lib/linuxadmin/updates/last.json":                       `{"state":"ok"}`,
		"/var/lib/linuxadmin/updates/staging/0.3.0-1/partial":         "x",
		"/var/lib/linuxadmin/firewall":                                "ufw 9443/tcp\n",
		"/etc/pam.d/linuxadmin":                                       legacyPAM,
		"/etc/systemd/system/linuxadmin.service":                      LegacyInstallerUnitHeader + "\n[Service]\nExecStart=/usr/bin/linuxadmind\n",
		"/etc/systemd/system/linuxadmin.service.d/docker-bridge.conf": "# Written by install.sh: LinuxAdmin listens on a Docker bridge address,\n[Unit]\nAfter=docker.service\n",
		"/etc/systemd/system/linuxadmin.service.d/notes.txt":          "not a drop-in",
		"/usr/lib/linuxadmin/versions/0.2.0/bin/linuxadmind":          "daemon 0.2.0",
		"/usr/lib/linuxadmin/versions/0.2.0/bin/linuxadmin-bridge":    "bridge 0.2.0",
		"/usr/lib/linuxadmin/versions/0.2.0/web/index.html":           "old web",
		"/usr/lib/linuxadmin/versions/0.2.0/VERSION":                  "0.2.0\n",
	})
	for _, l := range [][2]string{
		{"versions/0.2.0", "/usr/lib/linuxadmin/current"},
		{"current/bin/linuxadmin-bridge", "/usr/lib/linuxadmin/linuxadmin-bridge"},
		{"/usr/lib/linuxadmin/current/bin/linuxadmind", "/usr/bin/linuxadmind"},
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, l[1])), 0o755)
		if err := os.Symlink(l[0], filepath.Join(root, l[1])); err != nil {
			t.Fatal(err)
		}
	}
	return Paths{Root: root}
}

// addRelease puts the Ervisio compatibility archive's contents where
// LinuxAdmin's updater installs it.
func addRelease(t *testing.T, p Paths, v string) {
	t.Helper()
	d := "/usr/lib/linuxadmin/versions/" + v
	put(t, p.Root, map[string]string{
		d + "/bin/linuxadmind":              "ervisiod " + v,
		d + "/bin/linuxadmin-bridge":        "ervisio-bridge " + v,
		d + "/web/index.html":               "new web",
		d + "/plugins/docker/manifest.json": "{}",
		d + "/packaging/ervisio.service":    "[Service]\nExecStart=/usr/bin/ervisiod\n",
		d + "/VERSION":                      v + "\n",
	})
	os.Chmod(filepath.Join(p.Root, d, "bin/linuxadmind"), 0o755)
	os.Chmod(filepath.Join(p.Root, d, "bin/linuxadmin-bridge"), 0o755)
}

func TestImportCopiesEverything(t *testing.T) {
	p := legacySystem(t)
	res, err := (&Import{Paths: p}).Run()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Config || !res.State || !res.PAM || !reflect.DeepEqual(res.DropIns, []string{"docker-bridge.conf"}) {
		t.Fatalf("result %+v", res)
	}
	conf := read(t, p.At("/etc/ervisio/ervisio.conf"))
	for _, want := range []string{
		"# Ervisio server configuration", "systemctl restart ervisio.", "as ervisio.conf.bak",
		`cert = "/etc/ervisio/certs/site.pem"`, `key = "/etc/ervisio/certs/site.key"`, `listen = "0.0.0.0:9443"`,
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("ervisio.conf lacks %q:\n%s", want, conf)
		}
	}
	if read(t, p.At("/etc/ervisio/ervisio.conf.bak")) != "listen = \"0.0.0.0:9090\"\n" {
		t.Error("the backup was not copied as ervisio.conf.bak")
	}
	if fi, err := os.Stat(p.At("/etc/ervisio/tls/self-signed.key")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("TLS key not copied with mode 0600: %v %v", fi, err)
	}
	if read(t, p.At("/etc/ervisio/certs/site.pem")) != "PEM" || !isRegular(p.At("/etc/ervisio/plugins-catalog.url")) {
		t.Error("configuration files missing")
	}
	if read(t, p.At("/var/lib/ervisio/plugins-state.json")) != `{"docker":{"enabled":true}}` ||
		!isRegular(p.At("/var/lib/ervisio/plugins/docker/manifest.json")) ||
		!isRegular(p.At("/var/lib/ervisio/updates/last.json")) || !isRegular(p.At("/var/lib/ervisio/firewall")) {
		t.Error("state not copied")
	}
	if exists(p.At("/var/lib/ervisio/updates/staging")) {
		t.Error("the download staging folder was copied")
	}
	pam := read(t, p.At("/etc/pam.d/ervisio"))
	if !strings.Contains(pam, "PAM service for Ervisio sign-in (/etc/pam.d/ervisio)") || !strings.Contains(pam, "opened by ervisiod's root") ||
		!strings.Contains(pam, "auth      include   system-login") {
		t.Errorf("PAM file:\n%s", pam)
	}
	if d := read(t, p.At("/etc/systemd/system/ervisio.service.d/docker-bridge.conf")); !strings.Contains(d, "Ervisio listens") || !strings.Contains(d, "After=docker.service") {
		t.Errorf("drop-in:\n%s", d)
	}
	if exists(p.At("/etc/systemd/system/ervisio.service.d/notes.txt")) {
		t.Error("a file that is not a drop-in was copied")
	}
	// LinuxAdmin is untouched.
	if read(t, p.At("/etc/linuxadmin/linuxadmin.conf")) != legacyConf || read(t, p.At("/etc/pam.d/linuxadmin")) != legacyPAM ||
		!isRegular(p.At("/var/lib/linuxadmin/updates/staging/0.3.0-1/partial")) {
		t.Error("LinuxAdmin's files changed")
	}
	if Pending(p) {
		t.Error("an unmarked import is pending")
	}
	// Repeating it changes nothing.
	again, err := (&Import{Paths: p}).Run()
	if err != nil || again.Any() {
		t.Fatalf("second import: %+v %v", again, err)
	}
}

func TestImportKeepsErvisioData(t *testing.T) {
	p := legacySystem(t)
	put(t, p.Root, map[string]string{
		"/etc/ervisio/ervisio.conf": "listen = \"127.0.0.1:1\"\n",
		"/etc/pam.d/ervisio":        "# admin's own\n",
	})
	// What a package creates: empty folders.
	os.MkdirAll(p.At("/var/lib/ervisio/plugins"), 0o755)
	res, err := (&Import{Paths: p}).Run()
	if err != nil {
		t.Fatal(err)
	}
	if res.Config || res.PAM || !res.State {
		t.Fatalf("result %+v", res)
	}
	if read(t, p.At("/etc/ervisio/ervisio.conf")) != "listen = \"127.0.0.1:1\"\n" || read(t, p.At("/etc/pam.d/ervisio")) != "# admin's own\n" {
		t.Error("Ervisio's own files were replaced")
	}
	if !isRegular(p.At("/var/lib/ervisio/plugins/docker/manifest.json")) {
		t.Error("state not imported into the empty folders")
	}
}

func TestImportAdminPAMCopiedAsIs(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	own := "#%PAM-1.0\n# my own, mentions linuxadmind\nauth required pam_unix.so\n"
	put(t, p.Root, map[string]string{"/etc/pam.d/linuxadmin": own, "/etc/linuxadmin/linuxadmin.conf": ""})
	if _, err := (&Import{Paths: p}).Run(); err != nil {
		t.Fatal(err)
	}
	if read(t, p.At("/etc/pam.d/ervisio")) != own {
		t.Error("an administrator's PAM file was edited")
	}
}

func TestMarkedImportUndoRetryFinalize(t *testing.T) {
	p := legacySystem(t)
	res, err := (&Import{Paths: p, Mark: true}).Run()
	if err != nil {
		t.Fatal(err)
	}
	if !Pending(p) {
		t.Fatal("marked import not pending")
	}
	// The daemon of the new version starts meanwhile: it must not touch it.
	if r, err := ImportOnStart(p, nil); err != nil || r.Any() {
		t.Fatalf("ImportOnStart during a transition: %+v %v", r, err)
	}
	Undo(p, res)
	for _, gone := range []string{"/etc/ervisio", "/var/lib/ervisio", "/etc/pam.d/ervisio", "/etc/systemd/system/ervisio.service.d"} {
		if exists(p.At(gone)) {
			t.Errorf("%s is still there after Undo", gone)
		}
	}
	// LinuxAdmin's configuration changes after the failed attempt; the next
	// attempt copies it again, and an interrupted one is replaced too.
	put(t, p.Root, map[string]string{"/etc/linuxadmin/linuxadmin.conf": "listen = \"0.0.0.0:7000\"\n"})
	if _, err := (&Import{Paths: p, Mark: true}).Run(); err != nil {
		t.Fatal(err)
	}
	put(t, p.Root, map[string]string{"/etc/linuxadmin/linuxadmin.conf": "listen = \"0.0.0.0:7001\"\n"})
	if _, err := (&Import{Paths: p, Mark: true}).Run(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p.At("/etc/ervisio/ervisio.conf")); got != "listen = \"0.0.0.0:7001\"\n" {
		t.Fatalf("unfinished import not replaced: %q", got)
	}
	if err := Finalize(p); err != nil {
		t.Fatal(err)
	}
	if Pending(p) || exists(p.At("/etc/ervisio/"+importMarker)) {
		t.Error("markers left after Finalize")
	}
	if !strings.Contains(read(t, p.At("/etc/linuxadmin/"+NoteFile)), "systemctl enable --now linuxadmin.service") {
		t.Error("no note in /etc/linuxadmin")
	}
	// Final now: LinuxAdmin's later changes are not copied over Ervisio's.
	put(t, p.Root, map[string]string{"/etc/linuxadmin/linuxadmin.conf": "listen = \"0.0.0.0:7002\"\n"})
	if r, err := (&Import{Paths: p, Mark: true}).Run(); err != nil || r.Config {
		t.Fatalf("finalized import replaced: %+v %v", r, err)
	}
	if exists(p.At("/etc/ervisio/" + NoteFile)) {
		t.Error("the note was copied into /etc/ervisio")
	}
}

func TestImportOnStart(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	if r, err := ImportOnStart(p, nil); err != nil || r.Any() {
		t.Fatalf("no LinuxAdmin: %+v %v", r, err)
	}
	p = legacySystem(t)
	r, err := ImportOnStart(p, nil)
	if err != nil || !r.Config || !r.State {
		t.Fatalf("%+v %v", r, err)
	}
	if Pending(p) || !isRegular(p.At("/etc/linuxadmin/"+NoteFile)) {
		t.Error("ImportOnStart is not final, or left no note")
	}
}

func TestMigrateUserDir(t *testing.T) {
	home := t.TempDir()
	if moved, err := MigrateUserDir(home); moved || err != nil {
		t.Fatalf("nothing to move: %v %v", moved, err)
	}
	put(t, home, map[string]string{
		".config/linuxadmin/prefs.json":                         `{"theme":"oled"}`,
		".config/linuxadmin/plugins/docker/registries.key.json": `{"pw":"x"}`,
		".config/linuxadmin-signing/team.key":                   "SECRET",
	})
	os.Chmod(filepath.Join(home, ".config/linuxadmin"), 0o700)
	moved, err := MigrateUserDir(home)
	if err != nil || !moved {
		t.Fatalf("%v %v", moved, err)
	}
	if read(t, filepath.Join(home, ".config/ervisio/prefs.json")) != `{"theme":"oled"}` {
		t.Error("prefs not copied")
	}
	if fi, _ := os.Stat(filepath.Join(home, ".config/ervisio/plugins/docker/registries.key.json")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Error("plugin settings not copied with their mode")
	}
	if fi, _ := os.Stat(filepath.Join(home, ".config/ervisio")); fi == nil || fi.Mode().Perm() != 0o700 {
		t.Error("folder mode not kept")
	}
	if exists(filepath.Join(home, ".config/ervisio-signing")) || !exists(filepath.Join(home, ".config/linuxadmin/prefs.json")) {
		t.Error("touched other folders, or removed the old one")
	}
	// Once only.
	put(t, home, map[string]string{".config/linuxadmin/prefs.json": `{"theme":"daylight"}`})
	if moved, err := MigrateUserDir(home); moved || err != nil {
		t.Fatalf("second run moved: %v %v", moved, err)
	}
	if read(t, filepath.Join(home, ".config/ervisio/prefs.json")) != `{"theme":"oled"}` {
		t.Error("second run overwrote Ervisio's preferences")
	}
	// A symlinked old folder is not followed.
	home2 := t.TempDir()
	os.MkdirAll(filepath.Join(home2, ".config"), 0o755)
	os.Symlink("/etc", filepath.Join(home2, ".config/linuxadmin"))
	if moved, _ := MigrateUserDir(home2); moved {
		t.Error("followed a symlink")
	}
}

// fakeSystemd records systemctl calls.
type fakeSystemd struct {
	calls   []string
	active  map[string]bool
	enabled map[string]bool
	fail    map[string]error
}

func (f *fakeSystemd) Systemctl(_ context.Context, args ...string) error {
	c := strings.Join(args, " ")
	f.calls = append(f.calls, c)
	if f.fail != nil && f.fail[c] != nil {
		return f.fail[c]
	}
	switch {
	case len(args) == 2 && args[0] == "start":
		f.active[args[1]] = true
	case len(args) == 2 && args[0] == "stop":
		f.active[args[1]] = false
	case args[0] == "enable":
		f.enabled[args[len(args)-1]] = true
	case args[0] == "disable":
		f.enabled[args[len(args)-1]] = false
	}
	return nil
}
func (f *fakeSystemd) IsActive(_ context.Context, u string) bool  { return f.active[u] }
func (f *fakeSystemd) IsEnabled(_ context.Context, u string) bool { return f.enabled[u] }

func newFakeSystemd() *fakeSystemd {
	return &fakeSystemd{active: map[string]bool{"linuxadmin.service": true}, enabled: map[string]bool{"linuxadmin.service": true}}
}

func readLast(t *testing.T, path string) update.Result {
	t.Helper()
	var r update.Result
	if err := json.Unmarshal([]byte(read(t, path)), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTransition(t *testing.T) {
	p := legacySystem(t)
	addRelease(t, p, "0.3.0")
	put(t, p.Root, map[string]string{
		"/etc/systemd/system/linuxadmin-update.timer":   "# Written by LinuxAdmin.\n[Unit]\nDescription=LinuxAdmin scheduled system update\n\n[Timer]\nOnCalendar=*-*-* 03:30:00\n",
		"/etc/systemd/system/linuxadmin-update.service": "# Written by LinuxAdmin (Software > Update all > schedule).\n[Service]\nExecStart=/usr/bin/pacman -Syu --noconfirm\n",
	})
	sd := newFakeSystemd()
	var healthWant string
	tr := &Transition{Paths: p, Version: "0.3.0", Systemd: sd,
		Health: func(_ context.Context, want string) error {
			healthWant = want
			// While Ervisio starts, the data is there and still marked.
			if !Pending(p) || !isRegular(p.At("/etc/ervisio/ervisio.conf")) {
				return errors.New("data missing at start")
			}
			return nil
		}}
	res := tr.Run(context.Background())
	if res.State != update.StateOK || res.From != "0.2.0" || res.To != "0.3.0" {
		t.Fatalf("result %+v", res)
	}
	if healthWant != "0.3.0" {
		t.Errorf("health checked for %q", healthWant)
	}
	wantCalls := []string{"daemon-reload", "stop linuxadmin.service", "start ervisio.service", "enable ervisio.service",
		"disable linuxadmin.service", "disable --now linuxadmin-update.timer", "daemon-reload", "enable --now ervisio-update.timer"}
	if !reflect.DeepEqual(sd.calls, wantCalls) {
		t.Errorf("systemctl calls\n got %q\nwant %q", sd.calls, wantCalls)
	}
	// The Ervisio layout.
	if l, _ := os.Readlink(p.At("/usr/lib/ervisio/current")); l != "versions/0.3.0" {
		t.Errorf("current -> %q", l)
	}
	if read(t, p.At("/usr/lib/ervisio/versions/0.3.0/bin/ervisiod")) != "ervisiod 0.3.0" ||
		read(t, p.At("/usr/lib/ervisio/versions/0.3.0/bin/ervisio-bridge")) != "ervisio-bridge 0.3.0" ||
		exists(p.At("/usr/lib/ervisio/versions/0.3.0/bin/linuxadmind")) {
		t.Error("binaries not installed under their new names")
	}
	if fi, _ := os.Stat(p.At("/usr/lib/ervisio/versions/0.3.0/bin/ervisiod")); fi == nil || fi.Mode().Perm() != 0o755 {
		t.Error("daemon not executable")
	}
	if l, _ := os.Readlink(p.At("/usr/bin/ervisiod")); l != p.At("/usr/lib/ervisio/current/bin/ervisiod") {
		t.Errorf("/usr/bin/ervisiod -> %q", l)
	}
	if !strings.HasPrefix(read(t, p.At("/etc/systemd/system/ervisio.service")), InstallerUnitHeader+"\n[Service]") {
		t.Error("unit not installed with the installer header")
	}
	// Data final, note written, schedule moved.
	if Pending(p) || !isRegular(p.At("/etc/linuxadmin/"+NoteFile)) {
		t.Error("import not finalized")
	}
	if tm := read(t, p.At("/etc/systemd/system/ervisio-update.timer")); !strings.Contains(tm, "Description=Ervisio scheduled system update") || !strings.Contains(tm, "03:30:00") {
		t.Errorf("timer:\n%s", tm)
	}
	if exists(p.At("/etc/systemd/system/linuxadmin-update.timer")) {
		t.Error("old timer left")
	}
	// LinuxAdmin can still be started: its layout is as it was.
	if l, _ := os.Readlink(p.At("/usr/lib/linuxadmin/current")); l != "versions/0.2.0" {
		t.Errorf("LinuxAdmin's current changed: %q", l)
	}
	if !isRegular(p.At("/etc/systemd/system/linuxadmin.service")) || !isRegular(p.At("/etc/linuxadmin/linuxadmin.conf")) {
		t.Error("LinuxAdmin's files removed")
	}
	for _, f := range []string{"/var/lib/linuxadmin/updates/last.json", "/var/lib/ervisio/updates/last.json"} {
		if r := readLast(t, p.At(f)); r.State != update.StateOK || r.To != "0.3.0" || r.PID != 0 {
			t.Errorf("%s: %+v", f, r)
		}
	}
	// Ervisio's own updater works from here: the layout is a normal one.
	nl := &update.Layout{LibDir: p.At(brand.LibDir), BinLink: p.At(brand.BinLink)}
	if nl.Kind() != update.KindVersioned || nl.Previous() != "" {
		t.Errorf("layout kind %s previous %q", nl.Kind(), nl.Previous())
	}
}

func TestTransitionNotEnabled(t *testing.T) {
	p := legacySystem(t)
	addRelease(t, p, "0.3.0")
	sd := newFakeSystemd()
	sd.enabled["linuxadmin.service"] = false
	tr := &Transition{Paths: p, Version: "0.3.0", Systemd: sd, Health: func(context.Context, string) error { return nil }}
	if res := tr.Run(context.Background()); res.State != update.StateOK {
		t.Fatal(res.Error)
	}
	if sd.enabled["ervisio.service"] {
		t.Error("enabled at boot although LinuxAdmin was not")
	}
}

func TestTransitionRollback(t *testing.T) {
	p := legacySystem(t)
	addRelease(t, p, "0.3.0")
	sd := newFakeSystemd()
	legacyBack := false
	tr := &Transition{Paths: p, Version: "0.3.0", Systemd: sd,
		Health:       func(context.Context, string) error { return errors.New("no answer") },
		LegacyHealth: func(context.Context) error { legacyBack = true; return nil }}
	res := tr.Run(context.Background())
	if res.State != update.StateRolledBack || !strings.Contains(res.Error, "no answer") {
		t.Fatalf("result %+v", res)
	}
	if !legacyBack || !sd.active["linuxadmin.service"] || sd.active["ervisio.service"] || !sd.enabled["linuxadmin.service"] {
		t.Errorf("services after rollback: active %v enabled %v", sd.active, sd.enabled)
	}
	for _, gone := range []string{"/usr/lib/ervisio", "/usr/bin/ervisiod", "/etc/ervisio", "/var/lib/ervisio", "/etc/pam.d/ervisio",
		"/etc/systemd/system/ervisio.service", "/etc/systemd/system/ervisio.service.d"} {
		if exists(p.At(gone)) {
			t.Errorf("%s left after the rollback", gone)
		}
	}
	if r := readLast(t, p.At("/var/lib/linuxadmin/updates/last.json")); r.State != update.StateRolledBack || r.From != "0.2.0" {
		t.Errorf("LinuxAdmin's last.json: %+v", r)
	}
	if !FailedBefore(p, "0.3.0") || FailedBefore(p, "0.3.1") {
		t.Error("failure not recorded for 0.3.0 only")
	}
	if exists(p.At("/usr/lib/linuxadmin/versions/0.3.0")) || !exists(p.At("/usr/lib/linuxadmin/versions/0.2.0")) {
		t.Error("the failed version was kept, or LinuxAdmin's own removed")
	}
	if err := StartTransition(context.Background(), p, "/x", "0.3.0", func(context.Context, string, []string) error {
		t.Error("started again after a failure")
		return nil
	}); err == nil {
		t.Error("StartTransition accepted a version that failed")
	}
	// A later attempt (LinuxAdmin's updater downloads it again) succeeds and
	// clears the failure.
	addRelease(t, p, "0.3.0")
	tr.Health = func(context.Context, string) error { return nil }
	if res := tr.Run(context.Background()); res.State != update.StateOK {
		t.Fatalf("retry: %+v", res)
	}
	if FailedBefore(p, "0.3.0") {
		t.Error("failure kept after a successful attempt")
	}
}

func TestTransitionRefusals(t *testing.T) {
	p := legacySystem(t)
	sd := newFakeSystemd()
	ok := func(context.Context, string) error { return nil }
	if res := (&Transition{Paths: p, Version: "0.3.0", Systemd: sd, Health: ok}).Run(context.Background()); res.State != update.StateFailed {
		t.Errorf("missing release folder: %+v", res)
	}
	addRelease(t, p, "0.3.0")
	sd.active["ervisio.service"] = true
	if res := (&Transition{Paths: p, Version: "0.3.0", Systemd: sd, Health: ok}).Run(context.Background()); res.State != update.StateFailed {
		t.Errorf("Ervisio running already: %+v", res)
	}
	if len(sd.calls) != 0 {
		t.Errorf("refusals called systemctl: %q", sd.calls)
	}
}

func TestLegacyVersionOf(t *testing.T) {
	p := Paths{}
	if v, ok := LegacyVersionOf(p, "/usr/lib/linuxadmin/versions/0.3.0/bin/linuxadmind"); !ok || v != "0.3.0" {
		t.Errorf("%q %v", v, ok)
	}
	for _, exe := range []string{"/usr/lib/ervisio/versions/0.3.0/bin/ervisiod", "/usr/bin/linuxadmind", "/usr/lib/linuxadmin/versions/.x/bin/linuxadmind", "/home/u/ervisio/server/bin/ervisiod"} {
		if _, ok := LegacyVersionOf(p, exe); ok {
			t.Errorf("%s taken for LinuxAdmin's layout", exe)
		}
	}
}

func TestRemoveLegacy(t *testing.T) {
	ctx := context.Background()
	p := legacySystem(t)
	sd := newFakeSystemd()
	if _, err := RemoveLegacy(ctx, p, sd, false, nil); err == nil {
		t.Error("removed a running LinuxAdmin")
	}
	sd.active["linuxadmin.service"] = false
	put(t, p.Root, map[string]string{"/usr/lib/linuxadmin/managed": "apt\n"})
	if _, err := RemoveLegacy(ctx, p, sd, false, nil); err == nil || !strings.Contains(err.Error(), "apt") {
		t.Errorf("packaged LinuxAdmin: %v", err)
	}
	os.Remove(p.At("/usr/lib/linuxadmin/managed"))
	put(t, p.Root, map[string]string{"/etc/pam.d/linuxadmin": legacyPAM})
	removed, err := RemoveLegacy(ctx, p, sd, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"/usr/lib/linuxadmin", "/usr/bin/linuxadmind", "/etc/systemd/system/linuxadmin.service",
		"/etc/systemd/system/linuxadmin.service.d", "/etc/pam.d/linuxadmin"} {
		if exists(p.At(gone)) {
			t.Errorf("%s not removed (removed: %q)", gone, removed)
		}
	}
	if !isRegular(p.At("/etc/linuxadmin/linuxadmin.conf")) || !isDir(p.At("/var/lib/linuxadmin/plugins")) || !isRegular(p.At("/etc/linuxadmin/"+NoteFile)) {
		t.Error("data removed without --purge, or no note")
	}
	if _, err := RemoveLegacy(ctx, p, sd, true, nil); err != nil {
		t.Fatal(err)
	}
	if exists(p.At("/etc/linuxadmin")) || exists(p.At("/var/lib/linuxadmin")) {
		t.Error("--purge kept the data")
	}
	// An administrator's own unit and PAM file are not ours to delete.
	p2 := legacySystem(t)
	put(t, p2.Root, map[string]string{"/etc/systemd/system/linuxadmin.service": "[Service]\nExecStart=/opt/mine\n", "/etc/pam.d/linuxadmin": "# mine\n"})
	if _, err := RemoveLegacy(ctx, p2, &fakeSystemd{active: map[string]bool{}, enabled: map[string]bool{}}, false, nil); err != nil {
		t.Fatal(err)
	}
	if !isRegular(p2.At("/etc/systemd/system/linuxadmin.service")) || !isRegular(p2.At("/etc/pam.d/linuxadmin")) {
		t.Error("removed files LinuxAdmin did not write")
	}
}

func TestRemoveLegacyWaitsForTransition(t *testing.T) {
	p := legacySystem(t)
	if _, err := (&Import{Paths: p, Mark: true}).Run(); err != nil {
		t.Fatal(err)
	}
	sd := &fakeSystemd{active: map[string]bool{}, enabled: map[string]bool{}}
	if _, err := RemoveLegacy(context.Background(), p, sd, false, nil); err == nil {
		t.Error("removed LinuxAdmin while a transition is pending")
	}
}

func TestMigrateScheduleLeavesForeignUnits(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	put(t, p.Root, map[string]string{
		"/etc/systemd/system/linuxadmin-update.timer":   "[Timer]\nOnCalendar=daily\n",
		"/etc/systemd/system/linuxadmin-update.service": "[Service]\n",
	})
	sd := newFakeSystemd()
	if err := MigrateSchedule(context.Background(), p, sd, nil); err != nil {
		t.Fatal(err)
	}
	if len(sd.calls) != 0 || exists(p.At("/etc/systemd/system/ervisio-update.timer")) {
		t.Error("moved a timer LinuxAdmin did not write")
	}
}
