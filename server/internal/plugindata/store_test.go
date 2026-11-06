package plugindata

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func TestPersistenceIsolationAndConcurrentRevisionProtection(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	first, err := store.Write("chats/one.json", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	reload, err := New(root).Read("chats/one.json")
	if err != nil || reload.Text != "first" || reload.Revision != first.Revision {
		t.Fatalf("lost persisted document: %v %v", reload, err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, text := range []string{"second", "third"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := store.Write("chats/one.json", text, first.Revision)
			outcomes <- err
		}(text)
	}
	wg.Wait()
	close(outcomes)
	wins, conflicts := 0, 0
	for err := range outcomes {
		if err == nil {
			wins++
		} else if rpc.IsCode(err, rpc.Conflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("stale view was not rejected: %d %d", wins, conflicts)
	}
	if err := store.Remove("chats/one.json", first.Revision); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatalf("stale delete: %v", err)
	}
	current, err := store.Read("chats/one.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("chats/one.json", current.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Write("chats/one.json", "resurrection", current.Revision); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatal("old view resurrected deleted conversation")
	}
	list, err := store.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("deleted entry remains: %v %v", list, err)
	}
	other := New(t.TempDir())
	if _, err := other.Read("chats/one.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("private namespaces overlap")
	}
}

func TestDocumentLeasesPreventCompetingTurnsAndExpireOnServerTime(t *testing.T) {
	store := New(t.TempDir())
	now := time.Unix(1000, 0)
	store.now = func() time.Time { return now }
	first, err := store.Write("chat", "first", "")
	if err != nil {
		t.Fatal(err)
	}
	alice, bob := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err = store.Claim("chat", alice); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim("chat", bob); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatal("competing view acquired active conversation")
	}
	if _, err = store.Write("chat", "second", first.Revision, bob); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatal("other view wrote behind the lease")
	}
	if err = store.Remove("chat", first.Revision); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatal("deleted an active conversation")
	}
	second, err := store.Write("chat", "second", first.Revision, alice)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Minute)
	if _, err = store.Claim("chat", bob); err != nil {
		t.Fatal(err)
	}
	if err = store.Release("chat", alice); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatal("old owner released new owner's lease")
	}
	if err = store.Release("chat", bob); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Write("chat", "finished", second.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateDocumentsRejectTraversalAndLinkedPayloads(t *testing.T) {
	store := New(t.TempDir())
	for _, key := range []string{"../a", "a/../b", "/etc/passwd", "a//b", "a/", "a\\b", "."} {
		if _, err := store.Write(key, "blocked", ""); !rpc.IsCode(err, rpc.Invalid) {
			t.Fatalf("accepted unsafe key %q: %v", key, err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	path, _, _ := store.paths("linked")
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("linked"); err == nil {
		t.Fatal("followed a linked payload")
	}
	if _, err := store.Write("linked", "replace", ""); err == nil {
		t.Fatal("replaced a linked payload")
	}
	body, _ := os.ReadFile(secret)
	if string(body) != "private" {
		t.Fatal("modified linked target")
	}
}

func TestPrivateDocumentSizeQuotaAndPortableIdentity(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Write("large", string(make([]byte, MaxDocument+1)), ""); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal("unbounded document")
	}
	for _, key := range []string{"Case", "case", "chats/one.json"} {
		if _, err := store.Write(key, key, ""); err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List()
	if err != nil || len(list) != 3 {
		t.Fatalf("portable keys collided: %v %v", list, err)
	}
	for _, entry := range list {
		document, err := store.Read(entry.Path)
		if err != nil || document.Text != entry.Path {
			t.Fatalf("wrong key returned: %v %v", document, err)
		}
	}
}

func TestPrivateStorageEnforcesAggregateAndDocumentCountQuotas(t *testing.T) {
	for _, kind := range []string{"bytes", "count"} {
		t.Run(kind, func(t *testing.T) {
			store := New(t.TempDir())
			count := MaxDocuments
			var size int64
			if kind == "bytes" {
				count = MaxTotal / MaxDocument
				size = MaxDocument
			}
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("item-%d", i)
				path, meta, _ := store.paths(key)
				body, _ := json.Marshal(map[string]string{"key": key})
				if err := os.WriteFile(meta, body, 0600); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(size)
				_ = file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.Write("overflow", "x", ""); !rpc.IsCode(err, rpc.Invalid) {
				t.Fatalf("quota not enforced: %v", err)
			}
		})
	}
}
