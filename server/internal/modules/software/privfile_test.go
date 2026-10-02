package software

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTempState points the root state files at a private temp folder owned by
// the test user, who plays root.
func useTempState(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	oldF, oldP, oldO := stateRoot, stateRootPublic, stateRootOwner
	stateRoot, stateRootPublic, stateRootOwner = d+"/tx.json", d+"/tx.public.json", os.Geteuid()
	t.Cleanup(func() { stateRoot, stateRootPublic, stateRootOwner = oldF, oldP, oldO })
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()
}

func TestStateFilesSplitLogAndModes(t *testing.T) {
	useTempState(t)
	r := &txRun{path: stateRoot, public: stateRootPublic}
	r.st = txState{Running: true, PID: os.Getpid(), Op: "install", Log: []string{"installing secret-tool"}}
	r.save(true)
	if m := mode(t, stateRoot); m.Perm() != 0o600 || !m.IsRegular() {
		t.Errorf("full state mode %v", m)
	}
	if m := mode(t, stateRootPublic); m.Perm() != 0o644 {
		t.Errorf("public state mode %v", m)
	}
	b, _ := os.ReadFile(stateRootPublic)
	if strings.Contains(string(b), "secret-tool") {
		t.Errorf("public summary holds the log: %s", b)
	}
	// A user bridge sees the summary, with an empty (not null) log.
	_, tx := readStatus()
	if tx == nil || tx.Log == nil || len(tx.Log) != 0 || tx.Op != "install" {
		t.Errorf("summary: %+v", tx)
	}
}

func TestReadStatusRejectsSpoofedFiles(t *testing.T) {
	useTempState(t)
	good, _ := json.Marshal(txState{Running: true, PID: os.Getpid(), Op: "upgrade", StartedAt: 1})
	// A file of the wrong owner is ignored (stateRootOwner is root in production).
	os.WriteFile(stateRootPublic, good, 0o644)
	stateRootOwner = os.Geteuid() + 1
	if busy, tx := readStatus(); busy || tx != nil {
		t.Errorf("foreign file trusted: %v %+v", busy, tx)
	}
	stateRootOwner = os.Geteuid()
	// A link planted under the state name is never followed.
	os.Remove(stateRootPublic)
	real := filepath.Join(filepath.Dir(stateRootPublic), "elsewhere.json")
	os.WriteFile(real, good, 0o644)
	if err := os.Symlink(real, stateRootPublic); err != nil {
		t.Fatal(err)
	}
	if busy, tx := readStatus(); busy || tx != nil {
		t.Errorf("link followed: %v %+v", busy, tx)
	}
}

func TestUserStatePathNeedsPrivateRuntimeDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root has no user state file")
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if p := userStatePath(); p != "" {
		t.Errorf("no runtime dir must mean no file, got %q", p)
	}
	shared := t.TempDir()
	os.Chmod(shared, 0o1777)
	t.Setenv("XDG_RUNTIME_DIR", shared)
	if p := userStatePath(); p != "" {
		t.Errorf("shared folder accepted: %q", p)
	}
	priv := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", priv)
	if p := userStatePath(); !strings.HasPrefix(p, priv+"/") {
		t.Errorf("private runtime dir refused: %q", p)
	}
}

func TestStateSaveNeverWritesThroughLinks(t *testing.T) {
	d := t.TempDir()
	victim := d + "/victim"
	os.WriteFile(victim, []byte("keep"), 0o600)
	os.Mkdir(d+"/state", 0o755)
	path := d + "/state/tx.json"
	os.Symlink(victim, path)
	r := &txRun{path: path}
	r.st = txState{Op: "upgrade", Log: []string{}}
	r.save(true)
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("victim overwritten: %q", b)
	}
	if !mode(t, path).IsRegular() {
		t.Error("the link was not replaced by the state file")
	}
	// A folder others can write to is refused altogether.
	shared := d + "/shared"
	os.Mkdir(shared, 0o777)
	os.Chmod(shared, 0o777)
	r = &txRun{path: shared + "/tx.json"}
	r.save(true)
	if _, err := os.Lstat(shared + "/tx.json"); err == nil {
		t.Error("state written into a shared folder")
	}
}

// fakePacmanDB makes a system database folder with local/ and sync/core.db.
func fakePacman(t *testing.T) (*pacman, string) {
	t.Helper()
	d := t.TempDir()
	os.MkdirAll(d+"/sysdb/local", 0o755)
	os.MkdirAll(d+"/sysdb/sync", 0o755)
	os.WriteFile(d+"/sysdb/sync/core.db", []byte("core"), 0o644)
	os.WriteFile(d+"/sysdb/sync/extra.db", []byte("extra"), 0o644)
	p := newPacman()
	p.dbOnce.Do(func() {})
	p.dbPath = d + "/sysdb"
	old := checkDBBase
	checkDBBase = func() (string, error) { return d + "/cache/ervisio", nil }
	t.Cleanup(func() { checkDBBase = old })
	return p, d
}

func TestCheckDBFreshAndReused(t *testing.T) {
	p, d := fakePacman(t)
	tmp, err := p.prepareCheckDB()
	if err != nil {
		t.Fatal(err)
	}
	if tmp != d+"/cache/ervisio/checkdb" || mode(t, tmp).Perm() != 0o700 {
		t.Errorf("dir %s %v", tmp, mode(t, tmp))
	}
	if l, _ := os.Readlink(tmp + "/local"); l != d+"/sysdb/local" {
		t.Errorf("local link %q", l)
	}
	if b, _ := os.ReadFile(tmp + "/sync/extra.db"); string(b) != "extra" {
		t.Error("sync copy")
	}
	if args := p.dbArgs(); len(args) != 2 || args[1] != tmp {
		t.Errorf("dbArgs %v", args)
	}
	if _, err := p.prepareCheckDB(); err != nil {
		t.Errorf("reuse: %v", err)
	}
	p.forget()
	if _, err := os.Lstat(tmp); err == nil {
		t.Error("forget left the copy")
	}
}

func TestCheckDBRefusesPlantedPaths(t *testing.T) {
	t.Run("symlinked dir", func(t *testing.T) {
		p, d := fakePacman(t)
		os.MkdirAll(d+"/cache/ervisio", 0o700)
		os.Mkdir(d+"/attacker", 0o755)
		os.Symlink(d+"/attacker", d+"/cache/ervisio/checkdb")
		if _, err := p.prepareCheckDB(); err == nil {
			t.Error("a link in place of the folder was used")
		}
		if p.dbArgs() != nil {
			t.Error("dbArgs used a linked folder")
		}
		if ents, _ := os.ReadDir(d + "/attacker"); len(ents) != 0 {
			t.Errorf("wrote into the link target: %v", ents)
		}
	})
	t.Run("shared parent", func(t *testing.T) {
		p, d := fakePacman(t)
		os.MkdirAll(d+"/cache/ervisio", 0o700)
		os.Chmod(d+"/cache/ervisio", 0o1777)
		if _, err := p.prepareCheckDB(); err == nil {
			t.Error("a folder in a world-writable parent was used")
		}
	})
	t.Run("open mode", func(t *testing.T) {
		p, d := fakePacman(t)
		os.MkdirAll(d+"/cache/ervisio/checkdb/sync", 0o755)
		os.WriteFile(d+"/cache/ervisio/checkdb/sync/core.db", []byte("core"), 0o644)
		os.Chmod(d+"/cache/ervisio/checkdb", 0o755)
		if p.dbArgs() != nil {
			t.Error("dbArgs trusted a 0755 folder")
		}
		tmp, err := p.prepareCheckDB()
		if err != nil {
			t.Fatal(err)
		}
		if mode(t, tmp).Perm() != 0o700 {
			t.Errorf("mode not fixed: %v", mode(t, tmp))
		}
	})
	t.Run("planted entries", func(t *testing.T) {
		p, d := fakePacman(t)
		tmp := d + "/cache/ervisio/checkdb"
		os.MkdirAll(tmp+"/local", 0o700) // a folder instead of the link
		os.MkdirAll(tmp+"/sync", 0o700)
		os.WriteFile(d+"/victim", []byte("keep"), 0o600)
		os.Symlink(d+"/victim", tmp+"/sync/core.db")
		if _, err := p.prepareCheckDB(); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(d + "/victim"); string(b) != "keep" {
			t.Errorf("victim overwritten: %q", b)
		}
		if m := mode(t, tmp+"/sync/core.db"); !m.IsRegular() {
			t.Errorf("core.db is %v", m)
		}
		if b, _ := os.ReadFile(tmp + "/sync/core.db"); string(b) != "core" {
			t.Errorf("core.db %q", b)
		}
		if mode(t, tmp+"/local")&os.ModeSymlink == 0 {
			t.Error("local not replaced by the link")
		}
	})
}
