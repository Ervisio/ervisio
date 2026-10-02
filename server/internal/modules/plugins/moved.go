package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Plugins that used to ship inside Ervisio and now come from the
// marketplace (the signed catalog of the Ervisio/plugins registry).
//
// Up to 0.3.0 the Docker plugin was part of every release, in the packaged
// plugin folder, enabled by default. From 0.4.0 it is not: an update removes
// that folder. So that a machine that used it keeps it, the daemon (root)
// installs the marketplace version into InstalledDir once, at start:
//
//   - only when the plugin is not present anywhere, was not switched off in
//     plugins-state.json, and the host has what it is for (a Docker socket);
//   - only from the signed remote catalog, with the entry's sha256 and
//     capabilities as consent, and only a package whose signature verifies
//     against the team key (plugins.install does all of this);
//   - once: the outcome is kept in MovedStatePath ("installed" or
//     "skipped"); while it is "pending" (offline, catalog not reachable) the
//     daemon retries every hour, and Plugins shows a "moved to the
//     marketplace" card with an Install button in the meantime.
//
// The installer (install.sh) records "skipped" with --no-plugins, and runs
// `ervisiod --install-plugin docker` with --with-docker-plugin.

// MovedPlugin is a plugin that left the core.
type MovedPlugin struct {
	ID   string
	Name string
	// Wanted says whether the host looks like it uses the plugin.
	Wanted func() bool
}

// Moved are the plugins that moved out of the core, oldest first.
var Moved = []MovedPlugin{
	{ID: "docker", Name: "Docker", Wanted: dockerPresent},
}

// Outcomes kept in MovedStatePath.
const (
	MovedInstalled = "installed"
	MovedSkipped   = "skipped"
	MovedPending   = "pending"
	// MovedNotNeeded: the host did not use it (no Docker) when this
	// version first started. Not installed automatically later; Plugins
	// still shows the card if the host starts using it.
	MovedNotNeeded = "not-needed"
)

// MovedStatePath records what happened to each moved plugin on this machine.
var MovedStatePath = brand.StateDir + "/plugins-moved.json"

// DockerSockets are where a Docker engine listens (tests replace them).
var DockerSockets = []string{"/var/run/docker.sock", "/run/docker.sock"}

func dockerPresent() bool {
	for _, s := range DockerSockets {
		if fi, err := os.Stat(s); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return true
		}
	}
	return false
}

var movedMu sync.Mutex

// ReadMoved returns the recorded outcomes (empty when none).
func ReadMoved() map[string]string {
	out := map[string]string{}
	if b, err := os.ReadFile(MovedStatePath); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// SetMoved records the outcome for id.
func SetMoved(id, outcome string) error {
	movedMu.Lock()
	defer movedMu.Unlock()
	st := ReadMoved()
	if st[id] == outcome {
		return nil
	}
	st[id] = outcome
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(MovedStatePath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(MovedStatePath, append(b, '\n'), 0o644)
}

// MovedNotice is an entry of CatalogView.Moved.
type MovedNotice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// movedNotices lists the moved plugins the catalog offers, that are not
// installed, that the host looks like it wants, and that were not skipped.
func movedNotices(cat *Catalog, installed map[string]string) []MovedNotice {
	out := []MovedNotice{}
	st := ReadMoved()
	enabled := readState()
	for _, mp := range Moved {
		if _, ok := installed[mp.ID]; ok || st[mp.ID] == MovedSkipped {
			continue
		}
		if v, ok := enabled.Enabled[mp.ID]; ok && !v {
			continue
		}
		e := cat.find(mp.ID)
		if e == nil || e.Source == "" || !mp.Wanted() {
			continue
		}
		out = append(out, MovedNotice{ID: e.ID, Name: e.Name, Version: e.Version})
	}
	return out
}

// InstallFromCatalog installs (or updates) id from the signed remote
// catalog, with the catalog entry's checksum and capabilities as consent.
// It must run as root (it writes InstalledDir).
func InstallFromCatalog(ctx context.Context, id string) (*Info, error) {
	cat, err := loadRemoteCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if cat == nil {
		return nil, errors.New("no plugin catalog is configured (plugins.catalog_url is empty)")
	}
	e := cat.find(id)
	if e == nil || e.Source == "" {
		return nil, fmt.Errorf("the plugin catalog does not offer %q", id)
	}
	caps := e.Capabilities
	return install(ctx, installRequest{Source: e.Source, SHA256: e.SHA256, Consent: &caps})
}

// migrateMovedOnce handles every moved plugin and says whether one is
// still pending (to retry later).
func migrateMovedOnce(ctx context.Context, logf func(string, ...any)) (pending bool) {
	st := ReadMoved()
	pol := readPolicy()
	for _, mp := range Moved {
		switch st[mp.ID] {
		case MovedInstalled, MovedSkipped, MovedNotNeeded:
			continue
		}
		if find(pol, mp.ID) != nil {
			_ = SetMoved(mp.ID, MovedInstalled) // already there (installed by hand, or a dev folder)
			continue
		}
		if v, ok := readState().Enabled[mp.ID]; ok && !v {
			_ = SetMoved(mp.ID, MovedSkipped)
			logf("plugin %s moved to the marketplace; it was switched off here, so it is not installed", mp.ID)
			continue
		}
		if !mp.Wanted() {
			_ = SetMoved(mp.ID, MovedNotNeeded)
			continue
		}
		in, err := InstallFromCatalog(ctx, mp.ID)
		if err != nil {
			var re *rpc.Error
			if errors.As(err, &re) {
				err = errors.New(re.Message)
			}
			_ = SetMoved(mp.ID, MovedPending)
			logf("plugin %s moved to the marketplace; installing it failed (retrying later; Plugins offers an Install button): %v", mp.ID, err)
			pending = true
			continue
		}
		_ = SetMoved(mp.ID, MovedInstalled)
		logf("plugin %s moved to the marketplace: installed %s %s into %s", mp.ID, in.Name, in.Version, InstalledDir)
	}
	return pending
}

// MigrateMoved runs at daemon start (as root): it installs the
// marketplace version of plugins that left the core (see the top of this
// file), retrying every hour while one is pending, until ctx ends.
func MigrateMoved(ctx context.Context, logf func(string, ...any)) {
	for {
		if !migrateMovedOnce(ctx, logf) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}
