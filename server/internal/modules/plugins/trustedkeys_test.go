package plugins

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
)

func TestConfiguredPluginSigningTrust(t *testing.T) {
	previous := configmod.Path
	t.Cleanup(func() { configmod.Path = previous })
	system, _ := setup(t)
	pub, private := testKey(95)
	key := base64.StdEncoding.EncodeToString(pub)
	manifest := strings.Replace(goodManifest, `"visibleTo": {"groups": ["docker"]}`, `"visibleTo": {"groups": []}`, 1)
	dir := filepath.Join(system, "demo")
	code := "export default function() {}"
	writePlugin(t, dir, manifest, map[string]string{"index.js": code})
	if err := SignFolder(dir, private); err != nil {
		t.Fatal(err)
	}
	writeConfig := func(text string) {
		t.Helper()
		if err := os.WriteFile(configmod.Path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("[plugins]\nallow_unsigned = false\n")
	if _, err := Resolve("demo"); err == nil {
		t.Fatal("an unconfigured plugin signer was trusted")
	}
	writeConfig(fmt.Sprintf("[plugins]\ntrusted_keys = [%q]\n", key))
	keys, err := LoadPluginSigningKeys(configmod.Path)
	if err != nil || len(keys) != len(Keys())+1 {
		t.Fatalf("additional key missing: %v (%d keys)", err, len(keys))
	}
	for n, builtin := range Keys() {
		if !keys[n].Equal(builtin) {
			t.Fatal("the embedded team trust was replaced")
		}
	}
	if _, err := Resolve("demo"); err != nil {
		t.Fatalf("configured signed plugin rejected: %v", err)
	}
	catalog := []byte(`{"plugins":[],"categories":[]}`)
	sig, err := SignCatalog(catalog, private)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCatalog(catalog, sig, catalogKeys(readPolicy())); err == nil {
		t.Fatal("plugin-only trust unexpectedly authorized a catalog signer")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("demo"); err == nil {
		t.Fatal("configured signer bypassed file hashes")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfig(fmt.Sprintf("[plugins]\ncatalog_key = %q\n", key))
	if _, err := Resolve("demo"); err == nil {
		t.Fatal("catalog-only trust authorized a plugin or revoked key stayed cached")
	}
	if err := VerifyCatalog(catalog, sig, catalogKeys(readPolicy())); err != nil {
		t.Fatal("existing catalog_key behavior changed")
	}
	writeConfig("[plugins]\ntrusted_keys = [\"invalid\"]\n")
	if _, err := LoadPluginSigningKeys(configmod.Path); err == nil {
		t.Fatal("invalid signing configuration was accepted at startup")
	}
	if got := pluginSigningKeys(); len(got) != 0 {
		t.Fatal("invalid config retained previously configured signing authority")
	}
	if _, err := Resolve("demo"); err == nil {
		t.Fatal("plugin remained authorized after the trust config became invalid")
	}
}
