package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginTrustedKeysValidationAndRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for name, value := range map[string]any{
		"not a list":         key,
		"empty key":          []string{""},
		"not base64":         []string{"invalid!"},
		"private key length": []string{base64.StdEncoding.EncodeToString(make([]byte, 64))},
		"duplicates":         []string{key, key},
		"too many":           make([]string, 17),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Default().Set("plugins.trusted_keys", value); err == nil {
				t.Fatal("invalid signing trust accepted")
			}
		})
	}
	c := Default()
	if err := c.Set("plugins.trusted_keys", []any{key}); err != nil {
		t.Fatal(err)
	}
	clone := c.Clone()
	clone.Plugins.TrustedKeys[0] = "changed"
	if c.Plugins.TrustedKeys[0] != key {
		t.Fatal("Clone shares its signing trust list")
	}
	path := filepath.Join(t.TempDir(), "ervisio.conf")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, _, warnings, err := Load(path)
	if err != nil || len(warnings) != 0 || len(loaded.Plugins.TrustedKeys) != 1 || loaded.Plugins.TrustedKeys[0] != key {
		t.Fatalf("trusted key did not survive save/load: %v %v", err, warnings)
	}
	if c.Plugins.AllowUnsigned || c.Plugins.CatalogKey != "" {
		t.Fatal("private plugin trust changed unsigned or catalog policy")
	}
	if err := os.WriteFile(path, []byte("[plugins]\ntrusted_keys = [\"not-a-key\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "plugins.trusted_keys") {
		t.Fatalf("invalid TOML trust key was not rejected: %v", err)
	}
}
