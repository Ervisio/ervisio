package software

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	if m := mode(t, stateRoot); (runtime.GOOS != "windows" && m.Perm() != 0o600) || !m.IsRegular() { // no POSIX modes on Windows
		t.Errorf("full state mode %v", m)
	}
	if m := mode(t, stateRootPublic); runtime.GOOS != "windows" && m.Perm() != 0o644 {
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
	if runtime.GOOS != "windows" { // owners are uids; Windows trusts SYSTEM, Administrators and this user
		stateRootOwner = os.Geteuid() + 1
		if busy, tx := readStatus(); busy || tx != nil {
			t.Errorf("foreign file trusted: %v %+v", busy, tx)
		}
		stateRootOwner = os.Geteuid()
	}
	// A link planted under the state name is never followed.
	os.Remove(stateRootPublic)
	real := filepath.Join(filepath.Dir(stateRootPublic), "elsewhere.json")
	os.WriteFile(real, good, 0o644)
	if err := os.Symlink(real, stateRootPublic); err != nil {
		if runtime.GOOS == "windows" {
			return // creating a symlink needs a privilege the runner may lack
		}
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
	if runtime.GOOS != "windows" { // a world-writable mode is a Unix notion
		shared := t.TempDir()
		os.Chmod(shared, 0o1777)
		t.Setenv("XDG_RUNTIME_DIR", shared)
		if p := userStatePath(); p != "" {
			t.Errorf("shared folder accepted: %q", p)
		}
	}
	priv := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", priv)
	if p := userStatePath(); !strings.HasPrefix(p, priv+string(filepath.Separator)) {
		t.Errorf("private runtime dir refused: %q", p)
	}
}

func TestStateSaveNeverWritesThroughLinks(t *testing.T) {
	d := t.TempDir()
	victim := d + "/victim"
	os.WriteFile(victim, []byte("keep"), 0o600)
	os.Mkdir(d+"/state", 0o755)
	path := d + "/state/tx.json"
	linked := os.Symlink(victim, path) == nil
	if !linked && runtime.GOOS != "windows" { // Windows needs a privilege to link
		t.Fatal("cannot create the symlink")
	}
	r := &txRun{path: path}
	r.st = txState{Op: "upgrade", Log: []string{}}
	r.save(true)
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("victim overwritten: %q", b)
	}
	if !mode(t, path).IsRegular() {
		t.Error("the link was not replaced by the state file")
	}
	if runtime.GOOS == "windows" {
		return // "a folder others can write to" is a Unix mode check
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
