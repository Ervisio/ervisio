package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func TestStore(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "prefs.json")
	s := NewStore(p)
	if m, err := s.Get(); err != nil || len(m) != 0 {
		t.Fatal(m, err)
	}
	if _, err := s.Update(map[string]json.RawMessage{"theme": json.RawMessage(`"oled"`), "density": json.RawMessage(`1`)}, false); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Update(map[string]json.RawMessage{"density": json.RawMessage(`null`)}, false)
	if string(m["theme"]) != `"oled"` || m["density"] != nil {
		t.Fatalf("%v", m)
	}
	fi, _ := os.Stat(p)
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 { // no POSIX modes on Windows
		t.Fatalf("mode %v", fi.Mode())
	}
	if _, err := s.Update(map[string]json.RawMessage{"../x": json.RawMessage(`1`)}, false); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
	if _, err := s.Update(map[string]json.RawMessage{"x": json.RawMessage(`{bad`)}, false); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
	m, _ = s.Update(map[string]json.RawMessage{"lang": json.RawMessage(`"it"`)}, true)
	if len(m) != 1 {
		t.Fatalf("replace: %v", m)
	}
	_ = os.WriteFile(p, []byte("{corrupt"), 0o600)
	if m, err := s.Get(); err != nil || len(m) != 0 {
		t.Fatal(m, err)
	}
	if _, err := os.Stat(p + ".corrupt"); err != nil {
		t.Fatal("corrupt file not kept")
	}
}

func TestDesktopPersistsPerUserWithoutCreatingVirtualFolders(t *testing.T) {
	base := t.TempDir()
	alicePath := filepath.Join(base, "alice", "prefs.json")
	bobPath := filepath.Join(base, "bob", "prefs.json")
	desktop := json.RawMessage(`{"version":1,"folders":[{"id":"work","name":"Projects"}],"shortcuts":[{"app":"files","folder":"work"}]}`)
	alice, bob := NewStore(alicePath), NewStore(bobPath)
	if _, err := alice.Update(map[string]json.RawMessage{"shell.mode": json.RawMessage(`"ervisios"`), "shell.desktop": desktop}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := bob.Update(map[string]json.RawMessage{"shell.mode": json.RawMessage(`"classic"`)}, false); err != nil {
		t.Fatal(err)
	}
	// New bridge/store instances read the same account's saved desktop.
	a, err := NewStore(alicePath).Get()
	if err != nil || string(a["shell.mode"]) != `"ervisios"` {
		t.Fatalf("alice: %v %v", a, err)
	}
	var saved, want any
	_ = json.Unmarshal(a["shell.desktop"], &saved)
	_ = json.Unmarshal(desktop, &want)
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("desktop lost: %s", a["shell.desktop"])
	}
	b, err := NewStore(bobPath).Get()
	if err != nil || string(b["shell.mode"]) != `"classic"` || b["shell.desktop"] != nil {
		t.Fatalf("bob received another user's desktop: %v %v", b, err)
	}
	entries, err := os.ReadDir(filepath.Dir(alicePath))
	if err != nil || len(entries) != 1 || entries[0].Name() != "prefs.json" {
		t.Fatalf("virtual desktop created filesystem folders: %v %v", entries, err)
	}
}
