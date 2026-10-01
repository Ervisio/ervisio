package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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
	if fi.Mode().Perm() != 0o600 {
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
	os.WriteFile(p, []byte("{corrupt"), 0o600)
	if m, err := s.Get(); err != nil || len(m) != 0 {
		t.Fatal(m, err)
	}
	if _, err := os.Stat(p + ".corrupt"); err != nil {
		t.Fatal("corrupt file not kept")
	}
}
