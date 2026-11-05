package plugins

import (
	"crypto/ed25519"
	"fmt"

	cfg "github.com/ervisio/ervisio/server/internal/config"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/signkey"
)

// LoadPluginSigningKeys reads only the daemon's configuration file. The daemon
// passes this same absolute, administrator-owned path to user and root bridges;
// preferences and plugin requests never participate in the trust decision.
// Embedded team keys remain trusted. Additional keys apply to plugin packages,
// not to catalog signatures (plugins.catalog_key remains separate).
func LoadPluginSigningKeys(path string) ([]ed25519.PublicKey, error) {
	c, _, _, err := cfg.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load plugin signing trust: %w", err)
	}
	keys := append([]ed25519.PublicKey(nil), Keys()...)
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		seen[string(key)] = true
	}
	for n, text := range c.Plugins.TrustedKeys {
		key, err := signkey.ParsePublic(text)
		if err != nil {
			return nil, fmt.Errorf("plugins.trusted_keys: invalid public key at entry %d", n+1)
		}
		if !seen[string(key)] {
			keys = append(keys, key)
			seen[string(key)] = true
		}
	}
	return keys, nil
}

// Re-read at each authorization, including daemon-owned jobs. Revocation takes
// effect without waiting for an existing bridge to restart. Invalid/unreadable
// configuration supplies no verification keys, rather than retaining old trust.
func pluginSigningKeys() []ed25519.PublicKey {
	keys, err := LoadPluginSigningKeys(configmod.Path)
	if err != nil {
		return nil
	}
	return keys
}
