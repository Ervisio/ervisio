//go:build !windows

package software

import (
	"os"
	"testing"
)

// pacman keeps its check database in private folders with symlinks and
// 0700 modes; it exists on Linux only.

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
