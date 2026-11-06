package plugins

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedLookupVerifiesOnlyRequestedPluginAndRechecksChanges(t *testing.T) {
	system, installed := setup(t)
	pub, private, _ := ed25519.GenerateKey(rand.Reader)
	oldKeys := Keys
	calls := 0
	Keys = func() []ed25519.PublicKey { calls++; return []ed25519.PublicKey{pub} }
	t.Cleanup(func() { Keys = oldKeys })
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("other-%d", i)
		writePlugin(t, filepath.Join(system, id), strings.ReplaceAll(goodManifest, `"demo"`, `"`+id+`"`), map[string]string{"index.js": "unrelated"})
	}
	dir := filepath.Join(installed, "demo")
	writePlugin(t, dir, goodManifest, map[string]string{"index.js": "original"})
	if err := SignFolder(dir, private); err != nil {
		t.Fatal(err)
	}
	f := find(policy{}, "demo")
	if f == nil || !f.Sig.Verified || calls != 1 {
		t.Fatalf("lookup=%+v signature checks=%d", f, calls)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("changed!"), 0o644); err != nil {
		t.Fatal(err)
	}
	f = find(policy{}, "demo")
	if f == nil || f.Sig.Verified || calls != 2 {
		t.Fatalf("changed file must fail verification: %+v checks=%d", f, calls)
	}
	if find(policy{}, "../demo") != nil {
		t.Fatal("path traversal accepted")
	}
}

func TestScopedLookupKeepsDeveloperAndInvalidFolderPrecedence(t *testing.T) {
	system, installed := setup(t)
	writePlugin(t, filepath.Join(installed, "demo"), goodManifest, map[string]string{"index.js": "installed"})
	dev := t.TempDir()
	DevDirs = []string{dev}
	writePlugin(t, filepath.Join(dev, "arbitrary-name"), goodManifest, map[string]string{"index.js": "dev"})
	if f := find(policy{}, "demo"); f == nil || f.Location != LocDev || f.Name != "arbitrary-name" {
		t.Fatalf("developer precedence: %+v", f)
	}
	if err := os.WriteFile(filepath.Join(dev, "arbitrary-name", "manifest.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f := find(policy{}, "demo"); f == nil || f.Location != LocInstalled {
		t.Fatalf("invalid unrelated developer metadata: %+v", f)
	}
	writePlugin(t, filepath.Join(system, "demo"), "{", nil)
	if f := find(policy{}, "demo"); f != nil {
		t.Fatalf("invalid higher priority folder must shadow installed copy: %+v", f)
	}
}

// Compare scoped authorization with the old whole-inventory path. The same
// signed target is verified in both; only unrelated plugin work differs.
func BenchmarkPluginLookup(b *testing.B) {
	root := b.TempDir()
	oldSystem, oldInstalled, oldDev, oldKeys := SystemDir, InstalledDir, DevDirs, Keys
	SystemDir, InstalledDir, DevDirs = filepath.Join(root, "system"), filepath.Join(root, "installed"), []string{}
	pub, private, _ := ed25519.GenerateKey(rand.Reader)
	Keys = func() []ed25519.PublicKey { return []ed25519.PublicKey{pub} }
	b.Cleanup(func() { SystemDir, InstalledDir, DevDirs, Keys = oldSystem, oldInstalled, oldDev, oldKeys })
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("plugin-%d", i)
		dir := filepath.Join(InstalledDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		manifest := strings.ReplaceAll(goodManifest, `"demo"`, `"`+id+`"`)
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(strings.Repeat("x", 512<<10)), 0o644); err != nil {
			b.Fatal(err)
		}
		if err := SignFolder(dir, private); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("scoped", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if f := find(policy{}, "plugin-11"); f == nil || !f.Sig.Verified {
				b.Fatal("target verification failed")
			}
		}
	})
	b.Run("whole-inventory", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if len(scan(policy{})) != 12 {
				b.Fatal("incomplete inventory")
			}
		}
	})
}
