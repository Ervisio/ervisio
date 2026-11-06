package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/modules/prefs"
)

func privateStateTemp(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private")
	if err := createStateDirectory(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPersonalPreferencesMigrateOnceAndPersistWithoutHomeWrites(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "prefs.json")
	if err := os.WriteFile(source, []byte(`{"theme":"midnight","shell.mode":"classic"}`), 0400); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(privateStateTemp(t), "prefs.json")
	entry := &personalPreferences{store: prefs.NewStore(target)}
	reads := 0
	load := func() (json.RawMessage, error) { reads++; data, err := os.ReadFile(source); return data, err }
	if err := entry.initialize(target, load); err != nil {
		t.Fatal(err)
	}
	if _, err := entry.store.Update(map[string]json.RawMessage{"shell.mode": json.RawMessage(`"ervisios"`)}, false); err != nil {
		t.Fatal(err)
	}
	reloaded := &personalPreferences{store: prefs.NewStore(target)}
	if err := reloaded.initialize(target, func() (json.RawMessage, error) { return nil, errors.New("home no longer readable") }); err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if string(got["shell.mode"]) != `"ervisios"` || string(got["theme"]) != `"midnight"` || reads != 1 {
		t.Fatalf("lost migrated preferences: %v, reads %d", got, reads)
	}
	original, _ := os.ReadFile(source)
	if string(original) != `{"theme":"midnight","shell.mode":"classic"}` {
		t.Fatal("migration wrote to legacy preferences")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(target)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe file mode: %v", info.Mode())
		}
	}
}

func TestPersonalPreferencesNeverMixAccountsOrHonorClientIdentity(t *testing.T) {
	srv := &Server{opts: Options{StateDir: privateStateTemp(t)}}
	a := &Session{Account: &account.Account{Name: "alice", UID: 1000}}
	b := &Session{Account: &account.Account{Name: "bob", UID: 1001}}
	for _, sess := range []*Session{a, b} {
		dir, err := srv.accountStateDirectory(sess)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = prefs.NewStore(filepath.Join(dir, "prefs.json")).Update(map[string]json.RawMessage{}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.personalPreferencesCall(context.Background(), a, "prefs.set", json.RawMessage(`{"key":"shell.mode","value":"ervisios","user":"bob","uid":1001}`)); err != nil {
		t.Fatal(err)
	}
	av, err := srv.personalPreferencesCall(context.Background(), a, "prefs.get", nil)
	if err != nil {
		t.Fatal(err)
	}
	bv, err := srv.personalPreferencesCall(context.Background(), b, "prefs.get", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(av.(map[string]json.RawMessage)["shell.mode"]) != `"ervisios"` || len(bv.(map[string]json.RawMessage)) != 0 {
		t.Fatal("account isolation failed")
	}
}

func TestPersonalStateWindowsSIDIsCanonical(t *testing.T) {
	a, _ := accountStateID(&Session{Account: &account.Account{Name: "Alice", UID: 1000, SID: "s-1-5-21-12"}})
	b, _ := accountStateID(&Session{Account: &account.Account{Name: "ALICE", UID: 2000, SID: "S-1-5-21-12"}})
	c, _ := accountStateID(&Session{Account: &account.Account{Name: "Alice", UID: 1000, SID: "S-1-5-21-13"}})
	if a != b || a == c {
		t.Fatal("SID namespaces are not canonical")
	}
}

func TestPersonalMigrationPreservesUnreadableHistoryAndRejectsLinkedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	entry := &personalPreferences{store: prefs.NewStore(path)}
	if err := entry.initialize(path, func() (json.RawMessage, error) { return nil, os.ErrPermission }); err == nil {
		t.Fatal("unreadable history silently replaced")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created empty replacement preferences")
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(target, []byte(`{"private":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := entry.initialize(path, func() (json.RawMessage, error) { return json.RawMessage(`{}`), nil }); err == nil {
		t.Fatal("followed linked account state")
	}
}
