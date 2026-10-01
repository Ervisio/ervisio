package software

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

func TestParseHumanSize(t *testing.T) {
	cases := map[string]int64{"14.1 MB": 14784921, "608,2 MB": 637743923, "1 KiB": 1024, "12 B": 12, "2 GB": 2 << 30, "14.1 MB": 14784921, "": 0, "x": 0}
	for in, want := range cases {
		if got := parseHumanSize(in); got != want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseKV(t *testing.T) {
	f := parseKV("Name            : bash\nVersion         : 5.3.20-1\nDepends On      : readline  glibc\nOptional Deps   : bash-completion: for tab completion\n                  other: more\nRequired By     : None\n\nName : second\n")
	if fieldValue(f, "name") != "bash" || fieldValue(f, "Version") != "5.3.20-1" {
		t.Fatalf("fields %+v", f)
	}
	if v := fieldValue(f, "Optional Deps"); v != "bash-completion: for tab completion other: more" {
		t.Errorf("continuation: %q", v)
	}
	if fieldValue(f, "Required By") != "" || len(f) != 4 {
		t.Errorf("None values and later stanzas must be skipped: %+v", f)
	}
}

func TestNeedsReboot(t *testing.T) {
	for _, n := range []string{"linux", "linux-lts", "linux-zen", "systemd", "glibc", "linux-image-6.8.0-45-generic", "kernel-core", "libc6"} {
		if !needsReboot(n) {
			t.Errorf("%s should need a reboot", n)
		}
	}
	for _, n := range []string{"linux-firmware", "linux-headers", "firefox", "systemd-libs", "linux-api-headers"} {
		if needsReboot(n) {
			t.Errorf("%s should not need a reboot", n)
		}
	}
}

func TestValidNames(t *testing.T) {
	for _, n := range []string{"firefox", "libstdc++", "python-3.12", "com.spotify.Client", "gcc-libs:amd64", "a_b@c"} {
		if err := validNames([]string{n}); err != nil {
			t.Errorf("%q rejected: %v", n, err)
		}
	}
	for _, n := range []string{"", "-rf", "--noconfirm", "a b", "a;b", "$(x)", "../x", "a/b"} {
		if err := validNames([]string{n}); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
}

const pacmanQu = `appstream 1.2.0-1 -> 1.2.1-1
linux 7.2.6.arch2-1 -> 7.2.7.arch1-1
foo 1:1.0-1 -> 1:1.1-1 [ignored]
`

func TestParseQuAndSup(t *testing.T) {
	ups := parseQu(pacmanQu)
	if len(ups) != 2 || ups[1].Name != "linux" || ups[1].From != "7.2.6.arch2-1" || ups[1].To != "7.2.7.arch1-1" {
		t.Fatalf("%+v", ups)
	}
	sz := parseSup("tzdata|2026e-1|312965|core\nlinux|7.2.7.arch1-1|148000000|core\nnoise\n")
	if sz["linux"].size != 148000000 || sz["tzdata"].repo != "core" || len(sz) != 2 {
		t.Fatalf("%+v", sz)
	}
}

func TestParseLocalDesc(t *testing.T) {
	p := parseLocalDesc("%NAME%\nbash\n\n%VERSION%\n5.3.20-1\n\n%DESC%\nThe GNU Bourne Again shell\n\n%URL%\nhttps://x\n\n%INSTALLDATE%\n1790252092\n\n%REASON%\n1\n\n%SIZE%\n10056704\n")
	if p.Name != "bash" || p.Version != "5.3.20-1" || p.Reason != "dependency" || p.Size != 10056704 || p.InstallDate != 1790252092 || p.Description != "The GNU Bourne Again shell" {
		t.Fatalf("%+v", p)
	}
	if q := parseLocalDesc("%NAME%\nx\n\n%VERSION%\n1\n"); q.Reason != "explicit" {
		t.Errorf("missing %%REASON%% means explicit, got %q", q.Reason)
	}
}

func TestParseSyncList(t *testing.T) {
	m := parseSyncList("core bash 5.3.20-1 [installed]\nextra firefox 157.0-1\nmultilib firefox 1 \n")
	if m["bash"] != "core" || m["firefox"] != "extra" {
		t.Fatalf("%v", m)
	}
}

const pacmanSearch = `extra/firefox 156.0.1-1 [installed]
    Fast, Private & Safe Web Browser
extra/firefox-i18n-it 157.0-1
    Italian language pack
aur/firefox-nightly-bin 160.0a1-1 (+12 0.50) [Installed: 159.0a1-1]
    Nightly build
`

func TestParseSearch(t *testing.T) {
	r := parseSearch(pacmanSearch, KindRepo)
	if len(r) != 3 || r[0].Name != "firefox" || !r[0].Installed || r[0].Source != "extra" || r[0].Description != "Fast, Private & Safe Web Browser" {
		t.Fatalf("%+v", r)
	}
	if r[1].Installed || !r[2].Installed || r[2].Source != "aur" || r[2].Version != "160.0a1-1" {
		t.Fatalf("%+v", r)
	}
}

func TestParseServiceFiles(t *testing.T) {
	m := parseServiceFiles("docker /usr/lib/systemd/system/docker.service\ndocker /usr/lib/systemd/system/docker.socket\ndocker /usr/share/man/man1/docker-service-create.1.gz\nnginx /usr/lib/systemd/system/nginx.service\n")
	if !reflect.DeepEqual(m["docker"], []string{"docker.service"}) || !reflect.DeepEqual(m["nginx"], []string{"nginx.service"}) {
		t.Fatalf("%v", m)
	}
}

func TestParseOwned(t *testing.T) {
	m := parseOwned("/usr/share/applications/firefox.desktop is owned by firefox 156.0.1-1\n")
	if m["/usr/share/applications/firefox.desktop"] != "firefox" {
		t.Fatalf("%v", m)
	}
}

func TestPacmanProgress(t *testing.T) {
	var p Progress
	lines := []struct {
		in      string
		changed bool
		done    int
		total   int
		cur     string
	}{
		{":: Synchronizing package databases...", false, 0, 0, ""},
		{"Packages (18) systemd-259.3-1  openssl-3.5.4-1", true, 0, 18, ""},
		{"(1/18) checking keys in keyring", false, 0, 18, ""},
		{"(1/18) upgrading systemd", true, 0, 18, "systemd"},
		{"(4/18) upgrading linux", true, 3, 18, "linux"},
		{"(2/3) installing foo-1.0", true, 1, 3, "foo-1.0"},
		{"(5/5) Arming ConditionNeedsUpdate...", false, 1, 3, "foo-1.0"},
	}
	for _, c := range lines {
		if got := parsePacmanLine(c.in, &p); got != c.changed {
			t.Errorf("%q changed=%v, want %v", c.in, got, c.changed)
		}
		if c.changed && (p.Done != c.done || p.Total != c.total || (c.cur != "" && p.Current != c.cur)) {
			t.Errorf("%q -> %+v", c.in, p)
		}
	}
}

func TestPacmanPlans(t *testing.T) {
	p := newPacman()
	up, _ := p.Upgrade(nil)
	if got := up.Steps[0].Args; !reflect.DeepEqual(got, []string{"-Syu", "--noconfirm"}) {
		t.Errorf("%v", got)
	}
	in, _ := p.Install([]string{"gimp"})
	if got := strings.Join(in.Steps[0].Args, " "); got != "-S --noconfirm --needed -- gimp" {
		t.Errorf("%s", got)
	}
	rm, _ := p.Remove([]string{"gimp", "vim"})
	if got := strings.Join(rm.Steps[0].Args, " "); got != "-Rs --noconfirm -- gimp vim" {
		t.Errorf("%s", got)
	}
	if _, err := p.Install([]string{"--overwrite=*"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("option-looking name must be invalid, got %v", err)
	}
	if _, err := p.Install(nil); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("empty install must be invalid, got %v", err)
	}
}

const aptUpgradable = `Listing...
libssl3/jammy-updates,jammy-security 3.0.2-0ubuntu1.18 amd64 [upgradable from: 3.0.2-0ubuntu1.15]
linux-image-generic/jammy-updates 5.15.0.130.127 amd64 [upgradable from: 5.15.0.125.123]
vim/jammy-updates 2:8.2.3995-1ubuntu2.22 amd64 [upgradable from: 2:8.2.3995-1ubuntu2.21]
`

func TestParseApt(t *testing.T) {
	ups := parseAptUpgradable(aptUpgradable)
	if len(ups) != 3 {
		t.Fatalf("%+v", ups)
	}
	if !hasNote(ups[0], "security") || ups[0].From != "3.0.2-0ubuntu1.15" || ups[0].Source != "jammy-updates" {
		t.Errorf("%+v", ups[0])
	}
	if !hasNote(ups[1], "reboot") || hasNote(ups[2], "security") {
		t.Errorf("notes %+v %+v", ups[1], ups[2])
	}
	sz := parseAptURIs("'http://archive.ubuntu.com/ubuntu/pool/main/o/openssl/libssl3_3.0.2-0ubuntu1.18_amd64.deb' libssl3_3.0.2-0ubuntu1.18_amd64.deb 1905252 SHA256:abc\n")
	if sz["libssl3"] != 1905252 {
		t.Errorf("%v", sz)
	}
	pol := parseAptPolicy("vim:\n  Installed: (none)\n  Candidate: 2:8.2.3995-1\n  Version table:\ngit:\n  Installed: 1:2.34-1\n  Candidate: 1:2.34-2\n")
	if pol["vim"][0] != "(none)" || pol["git"][1] != "1:2.34-2" {
		t.Errorf("%v", pol)
	}
}

func TestParseDpkgList(t *testing.T) {
	out := "bash\t5.1-6\t1864\tii \tGNU Bourne Again SHell\nold\t1\t5\trc \tremoved config\nnginx\t1.18\t1100\tii \tHTTP server\n"
	p := parseDpkgList(out, map[string]bool{"nginx": true}, map[string]bool{"bash": true})
	if len(p) != 2 || p[0].Reason != "dependency" || !p[0].Orphan || p[1].Reason != "explicit" || p[0].Size != 1864*1024 {
		t.Fatalf("%+v", p)
	}
}

func TestAptProgress(t *testing.T) {
	var p Progress
	for _, l := range []string{"3 upgraded, 1 newly installed, 0 to remove and 0 not upgraded.", "Unpacking libssl3 (3.0.2-0ubuntu1.18) over (3.0.2-0ubuntu1.15) ...", "Setting up libssl3:amd64 (3.0.2-0ubuntu1.18) ..."} {
		parseAptLine(l, &p)
	}
	if p.Total != 4 || p.Done != 1 || p.Current != "libssl3" {
		t.Errorf("%+v", p)
	}
}

func TestDnf(t *testing.T) {
	out := "\nkernel-core.x86_64                 6.10.9-200.fc40      updates\nfirefox.x86_64   130.0-1.fc40   updates\nObsoleting Packages\nold.x86_64 1 updates\n"
	ups := parseDnfCheckUpdate(out)
	if len(ups) < 2 || ups[0].Name != "kernel-core" || !hasNote(ups[0], "reboot") || ups[1].To != "130.0-1.fc40" || ups[1].Source != "updates" {
		t.Fatalf("%+v", ups)
	}
	var p Progress
	parseDnfLine("  Upgrading        : firefox-130.0-1.fc40.x86_64                  7/42", &p)
	if p.Done != 6 || p.Total != 42 || p.Current != "firefox-130.0-1.fc40.x86_64" {
		t.Errorf("dnf4 %+v", p)
	}
	parseDnfLine("[3/10] Upgrading kernel-core-6.10.9-200.fc40.x86_64", &p)
	if p.Done != 2 || p.Total != 10 {
		t.Errorf("dnf5 %+v", p)
	}
	s := parseDnfSearch("=== Name & Summary Matched: gimp ===\ngimp.x86_64 : GNU Image Manipulation Program\ngimp-libs.x86_64 : GIMP libraries\n")
	if len(s) != 2 || s[0].Name != "gimp" {
		t.Errorf("%+v", s)
	}
	r := parseRpmList("bash\t5.2.26-3.fc40\t7690000\t1721000000\tThe GNU Bourne Again shell\ngpg-pubkey\t1\t0\t1\tx\ngimp\t2.10\t1000\t1721000001\tGIMP\n", "dnf", nil, false, map[string]bool{"gimp": true})
	if len(r) != 2 || r[0].Reason != "dependency" || r[1].Reason != "explicit" {
		t.Errorf("%+v", r)
	}
}

const zypperUpdates = `S | Repository | Name   | Current Version | Available Version | Arch
--+------------+--------+-----------------+-------------------+-------
v | Update     | kernel-default | 6.4.0-1 | 6.4.0-2 | x86_64
v | Main       | vim    | 9.0-1           | 9.0-2             | x86_64
`

func TestZypper(t *testing.T) {
	ups := parseZypperUpdates(zypperUpdates)
	if len(ups) != 2 || ups[0].Name != "kernel-default" || ups[0].From != "6.4.0-1" || ups[1].To != "9.0-2" || ups[1].Source != "Main" || !hasNote(ups[0], "reboot") {
		t.Fatalf("%+v", ups)
	}
	s := parseZypperSearch("S  | Name | Summary | Type\n---+------+---------+--------\ni  | vim  | Vi IMproved | package\n   | vim-data | data | package\n   | patt | pattern | pattern\n")
	if len(s) != 2 || !s[0].Installed || s[1].Installed {
		t.Errorf("%+v", s)
	}
	var p Progress
	parseZypperLine("(2/5) Installing: vim-9.0-2.x86_64 [done]", &p)
	if p.Done != 1 || p.Total != 5 || p.Current != "vim-9.0-2.x86_64" {
		t.Errorf("%+v", p)
	}
}

func TestFlatpak(t *testing.T) {
	pk := parseFlatpakList("com.spotify.Client\tSpotify\t1.2.95\tstable\tflathub\tsystem\t14.1 MB\tOnline music\norg.x.App\tX\t\tstable\tflathub\tuser\t2 GB\t\n")
	if len(pk) != 2 || pk[0].Title != "Spotify" || pk[0].Scope != "system" || pk[0].Size != parseHumanSize("14.1 MB") || pk[1].Version != "stable" || pk[1].Scope != "user" {
		t.Fatalf("%+v", pk)
	}
	ups := parseFlatpakUpdates("com.spotify.Client\tSpotify\t1.2.96\tflathub\t182.0 MB\n", "system", map[string]string{"system/com.spotify.Client": "1.2.95"})
	if len(ups) != 1 || ups[0].From != "1.2.95" || ups[0].To != "1.2.96" || ups[0].Kind != KindFlatpak || ups[0].Size != parseHumanSize("182 MB") {
		t.Fatalf("%+v", ups)
	}
	res := parseFlatpakSearch("Spotify\tOnline music\tcom.spotify.Client\t1.2.95\tflathub\n")
	if len(res) != 1 || res[0].Name != "com.spotify.Client" || res[0].Title != "Spotify" || res[0].Remote != "flathub" {
		t.Fatalf("%+v", res)
	}
	var p Progress
	if !parseFlatpakLine("Updating 2/5…", &p) || p.Done != 1 || p.Total != 5 {
		t.Errorf("%+v", p)
	}
	f := newFlatpak().WithScope("user")
	pl, err := f.Install([]string{"flathub/org.gimp.GIMP"})
	if err != nil || strings.Join(pl.Steps[0].Args, " ") != "install -y --noninteractive --user flathub org.gimp.GIMP" {
		t.Errorf("%v %+v", err, pl)
	}
	if _, err := f.Install([]string{"flathub/--user"}); err == nil {
		t.Error("option as app id must be rejected")
	}
	up, _ := newFlatpak().WithScope("system").Upgrade(nil)
	if strings.Join(up.Steps[0].Args, " ") != "update -y --noninteractive --system" {
		t.Errorf("%v", up.Steps[0].Args)
	}
}

func TestAURUnsupported(t *testing.T) {
	a := &aur{helper: "yay"}
	if _, err := a.Install([]string{"yay-bin"}); !rpc.IsCode(err, rpc.Unavailable) {
		t.Errorf("%v", err)
	}
	m := &manager{primary: newPacman(), aur: a}
	if _, err := m.buildPlans(TxParams{Op: "install", Packages: []string{"x"}, Source: "aur"}, true); !rpc.IsCode(err, rpc.Unavailable) {
		t.Errorf("AUR install: %v", err)
	}
	pl, err := m.buildPlans(TxParams{Op: "remove", Packages: []string{"x"}, Source: "aur"}, true)
	if err != nil || pl[0].Steps[0].Name != "pacman" {
		t.Errorf("AUR removal goes through pacman: %v %+v", err, pl)
	}
	ups := parseQu("claude-code 2.1.280-1 -> 2.1.287-1 [29m]\n")
	if len(ups) != 1 || ups[0].To != "2.1.287-1" {
		t.Errorf("%+v", ups)
	}
}

func TestBuildPlansRules(t *testing.T) {
	m := &manager{primary: newPacman(), flatpak: newFlatpak()}
	if _, err := m.buildPlans(TxParams{Op: "upgrade"}, false); !rpc.IsCode(err, rpc.NeedsAdmin) {
		t.Errorf("system upgrade without admin: %v", err)
	}
	if _, err := m.buildPlans(TxParams{Op: "upgrade", Source: "flatpak", Scope: "user"}, false); err != nil {
		t.Errorf("user flatpak: %v", err)
	}
	if _, err := m.buildPlans(TxParams{Op: "upgrade", Source: "flatpak", Scope: "user"}, true); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("user scope in the root bridge: %v", err)
	}
	if _, err := m.buildPlans(TxParams{Op: "format"}, true); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("unknown op: %v", err)
	}
	if _, err := m.buildPlans(TxParams{Op: "remove"}, true); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("remove nothing: %v", err)
	}
	all, err := m.buildPlans(TxParams{Op: "upgrade", Source: "all"}, true)
	if err != nil || len(all) != 2 || all[0].Steps[0].Name != "pacman" || all[1].Steps[0].Name != "flatpak" {
		t.Errorf("upgrade all: %v %+v", err, all)
	}
	if _, err := m.buildPlans(TxParams{Op: "install", Source: "all", Packages: []string{"x"}}, true); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("install from all: %v", err)
	}
}

func TestHistory(t *testing.T) {
	log := `[2026-10-01T20:29:59+0200] [PACMAN] Running 'pacman -Syu'
[2026-10-01T20:29:59+0200] [ALPM] transaction started
[2026-10-01T20:29:59+0200] [ALPM] upgraded linux (7.2.6.arch2-1 -> 7.2.7.arch1-1)
[2026-10-01T20:29:59+0200] [ALPM] installed xorg-xauth (1.1.5-1)
[2026-10-01T20:29:59+0200] [ALPM] removed foo (1.0-1)
[2026-10-01T20:29:59+0200] [ALPM] transaction completed
[2026-10-01T20:30:59+0200] [ALPM] transaction started
[2026-10-01T20:30:59+0200] [ALPM] downgraded bar (2-1 -> 1-1)
`
	h := parsePacmanLog(log)
	if len(h) != 4 || h[0].Action != "upgraded" || h[0].From != "7.2.6.arch2-1" || h[0].To != "7.2.7.arch1-1" || h[1].To != "1.1.5-1" || h[2].From != "1.0-1" || h[0].Tx != 1 || h[3].Tx != 2 {
		t.Fatalf("%+v", h)
	}
	if h[0].Time != time.Date(2026, 10, 1, 18, 29, 59, 0, time.UTC).UnixMilli() {
		t.Errorf("time %d", h[0].Time)
	}
	a := parseAptHistory("Start-Date: 2024-05-01  10:00:00\nCommandline: apt upgrade\nInstall: libfoo:amd64 (1.0, automatic), bar:amd64 (2.0)\nUpgrade: libssl3:amd64 (3.0.2-0ubuntu1.15, 3.0.2-0ubuntu1.18)\nRemove: old:amd64 (0.9)\nEnd-Date: 2024-05-01  10:01:00\n")
	if len(a) != 4 || a[0].Action != "installed" || a[0].To != "1.0" || a[2].From != "3.0.2-0ubuntu1.15" || a[2].To != "3.0.2-0ubuntu1.18" || a[3].Action != "removed" || a[0].Tx != 1 {
		t.Fatalf("%+v", a)
	}
	z := parseZypperHistory("# comment\n2024-05-01 10:00:00|install|vim|9.0-2|x86_64||Main|abc|\n2024-05-01 10:00:00|remove|nano|7.2|x86_64|root@h||\n2024-05-02 11:00:00|command|zypper up|\n")
	if len(z) != 2 || z[0].Tx != 1 || z[1].Action != "removed" || z[1].From != "7.2" {
		t.Fatalf("%+v", z)
	}
	d := parseDnfLog("2024-05-01T10:00:00Z SUBDEBUG Upgrade: firefox-130.0-1.fc40.x86_64\n2024-05-01T10:00:01Z SUBDEBUG Installed: gimp-2.10.38-1.fc40.x86_64\n2024-05-01T10:00:02Z INFO Erase: old-1-1.fc40.noarch\n")
	if len(d) != 3 || d[0].Name != "firefox" || d[0].To != "130.0-1.fc40" || d[2].Action != "removed" || d[0].Tx != d[1].Tx {
		t.Fatalf("%+v", d)
	}
}

func TestParseDesktop(t *testing.T) {
	a, ok := parseDesktop("[Desktop Entry]\nType=Application\nName=Firefox\nName[it]=Firefox it\nComment=Browse the Web\nIcon=firefox\nExec=firefox %u\nCategories=Network;WebBrowser;\n\n[Desktop Action new-window]\nName=New Window\n")
	if !ok || a.Name != "Firefox" || a.Icon != "firefox" || a.Comment != "Browse the Web" || !reflect.DeepEqual(a.Categories, []string{"Network", "WebBrowser"}) {
		t.Fatalf("%+v ok=%v", a, ok)
	}
	if _, ok := parseDesktop("[Desktop Entry]\nType=Application\nName=Hidden\nNoDisplay=true\n"); ok {
		t.Error("NoDisplay must be skipped")
	}
	if _, ok := parseDesktop("[Desktop Entry]\nType=Link\nName=Link\n"); ok {
		t.Error("links must be skipped")
	}
}

func TestIcons(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "hicolor/48x48/apps")
	svg := filepath.Join(dir, "hicolor/scalable/apps")
	os.MkdirAll(png, 0o755)
	os.MkdirAll(svg, 0o755)
	os.WriteFile(filepath.Join(png, "foo.png"), []byte("\x89PNG"), 0o644)
	os.WriteFile(filepath.Join(svg, "bar.svg"), []byte("<svg/>"), 0o644)
	if got := searchIconIn([]string{dir}, "foo", 64); !strings.HasSuffix(got, "48x48/apps/foo.png") {
		t.Errorf("png: %q", got)
	}
	if got := searchIconIn([]string{dir}, "bar", 64); !strings.HasSuffix(got, "scalable/apps/bar.svg") {
		t.Errorf("svg: %q", got)
	}
	if _, err := loadIcon("../../etc/passwd", 64); err == nil {
		t.Error("path traversal accepted")
	}
	if _, err := loadIcon("/etc/shadow", 64); err == nil {
		t.Error("absolute path outside the icon roots accepted")
	}
	if _, err := loadIcon("/usr/share/x/../../../etc/passwd.png", 64); err == nil {
		t.Error("cleaned path outside roots accepted")
	}
	dirs := iconDirs(64)
	if dirs[0] != "64x64/apps" || dirs[2] != "96x96/apps" {
		t.Errorf("size preference: %v", dirs[:6])
	}
}

func TestSchedule(t *testing.T) {
	old := systemdDir
	systemdDir = t.TempDir()
	defer func() { systemdDir = old }()
	if currentSchedule() != "" {
		t.Error("no timer yet")
	}
	os.WriteFile(filepath.Join(systemdDir, timerName), []byte(renderTimer("03:00")), 0o644)
	if got := currentSchedule(); got != "03:00" {
		t.Errorf("got %q", got)
	}
	m := &manager{primary: newPacman()}
	bad := "3am"
	if _, err := m.setSchedule(context.Background(), &bad); !rpc.IsCode(err, rpc.Invalid) {
		t.Errorf("bad time: %v", err)
	}
	svc, err := renderService([]Plan{{Steps: []Step{{Name: "sh", Args: []string{"-c", "echo a b"}, Env: []string{"X=1"}}}}, {Steps: []Step{{Name: "sh", Args: []string{"-c", "true"}}}}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(svc, `ExecStart=/`) || !strings.Contains(svc, `"echo a b"`) || !strings.Contains(svc, "ExecStart=-/") || !strings.Contains(svc, "Environment=X=1") || !strings.Contains(svc, "Type=oneshot") {
		t.Errorf("service:\n%s", svc)
	}
}

func TestHintFor(t *testing.T) {
	cases := map[string]string{
		"error: failed retrieving file 'x.pkg' from mirror : 404":     "outdated",
		"error: failed to init transaction (unable to lock database)": "locked",
		"error: failed to commit transaction (conflicting files)":     "conflict",
		"fine": "",
	}
	for in, want := range cases {
		if got := hintFor([]string{in}); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// fakeRun collects what a transaction would send.
func newFakeRun() (*txRun, *[]map[string]any) {
	var sent []map[string]any
	r := &txRun{}
	r.send = func(v any) bool {
		if m, ok := v.(map[string]any); ok {
			sent = append(sent, m)
		}
		return true
	}
	return r, &sent
}

func TestExecutePlansStreamsAndParses(t *testing.T) {
	r, sent := newFakeRun()
	r.path = filepath.Join(t.TempDir(), "state.json")
	script := "echo 'Packages (2) a-1 b-2'; echo '(1/2) upgrading a'; echo err >&2; printf '(2/2) upgrading b\\r'; echo done"
	pl := Plan{Steps: []Step{{Title: "upgrade", Name: "sh", Args: []string{"-c", script}, Parse: parsePacmanLine}}}
	ok, msg := executePlans([]Plan{pl}, r, "upgrade")
	if !ok || msg != "" {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	var progress []map[string]any
	var logs []string
	for _, m := range *sent {
		switch m["type"] {
		case "progress":
			progress = append(progress, m)
		case "log":
			logs = append(logs, m["line"].(string))
		}
	}
	if len(progress) != 3 || progress[2]["done"] != 1 || progress[2]["total"] != 2 || progress[2]["current"] != "b" {
		t.Errorf("progress %+v", progress)
	}
	if len(logs) < 5 || !strings.HasPrefix(logs[0], "$ sh") {
		t.Errorf("logs %v", logs)
	}
	if b, err := os.ReadFile(r.path); err != nil || !strings.Contains(string(b), `"current":"b"`) {
		t.Errorf("state file: %v %s", err, b)
	}
}

func TestExecutePlansFailure(t *testing.T) {
	r, _ := newFakeRun()
	pl := Plan{Steps: []Step{{Name: "sh", Args: []string{"-c", "echo 'error: target not found: nope' ; exit 1"}}, {Name: "sh", Args: []string{"-c", "echo never"}}}}
	ok, msg := executePlans([]Plan{pl}, r, "install")
	if ok || !strings.Contains(msg, "target not found") {
		t.Errorf("ok=%v msg=%q", ok, msg)
	}
	for _, l := range r.st.Log {
		if l == "never" {
			t.Error("second step ran after the first failed")
		}
	}
}

func TestReadStatus(t *testing.T) {
	useTempState(t)
	if busy, tx := readStatus(); busy || tx != nil {
		t.Fatal("idle expected")
	}
	r := &txRun{path: stateRoot, public: stateRootPublic}
	r.st = txState{Running: true, PID: os.Getpid(), Op: "upgrade", Total: 3, Done: 1, Log: []string{"x"}}
	r.save(true)
	if busy, tx := readStatus(); !busy || tx == nil || tx.Done != 1 {
		t.Errorf("running: %v %+v", busy, tx)
	}
	r.st.PID = 999999999 // a dead process
	r.save(true)
	if busy, tx := readStatus(); busy || tx == nil || tx.Running {
		t.Errorf("stale state must not be busy: %v %+v", busy, tx)
	}
}

func TestFilterInstalled(t *testing.T) {
	pk := []Package{
		{Name: "a", Reason: "explicit", Kind: KindRepo},
		{Name: "b", Reason: "dependency", Kind: KindRepo, Orphan: true},
		{Name: "c", Reason: "explicit", Kind: KindAUR},
		{Name: "d", Reason: "explicit", Kind: KindFlatpak},
	}
	count := func(f string) int { r, _ := filterInstalled(pk, f); return len(r) }
	if count("all") != 4 || count("explicit") != 2 || count("deps") != 1 || count("orphans") != 1 || count("aur") != 1 || count("flatpak") != 1 {
		t.Error("filters")
	}
	if _, err := filterInstalled(pk, "weird"); !rpc.IsCode(err, rpc.Invalid) {
		t.Error("unknown filter must be invalid")
	}
}

func TestCache(t *testing.T) {
	var c ttlCache
	n := 0
	f := func() (any, error) { n++; return n, nil }
	c.get("k", time.Minute, false, f)
	v, _, _ := c.get("k", time.Minute, false, f)
	if v.(int) != 1 || n != 1 {
		t.Error("second get must hit the cache")
	}
	v, _, _ = c.get("k", time.Minute, true, f)
	if v.(int) != 2 {
		t.Error("force must recompute")
	}
	c.clear()
	v, _, _ = c.get("k", time.Minute, false, f)
	if v.(int) != 3 {
		t.Error("clear must drop values")
	}
}

func TestFlatpakSearchDedupes(t *testing.T) {
	res := parseFlatpakSearch("GIMP\tImage editor\torg.gimp.GIMP\t3.0\tflathub\nGIMP\tImage editor\torg.gimp.GIMP\t3.0\tflathub-beta\n")
	if len(res) != 1 || res[0].Remote != "flathub" {
		t.Fatalf("%+v", res)
	}
}
