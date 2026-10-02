package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/brand"
	configmod "github.com/ervisio/ervisio/server/internal/modules/config"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/update"
)

func TestCheck(t *testing.T) {
	var path string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewEncoder(w).Encode([]map[string]any{
			{"tag_name": "v99.0.0-rc.1", "prerelease": true, "body": "# RC", "published_at": "2026-09-30T10:00:00Z",
				"assets": []map[string]any{{"name": update.ArchiveName("99.0.0-rc.1", update.Arch), "size": 1234, "browser_download_url": "https://github.com/x"}}},
		})
	}))
	defer srv.Close()
	old := checker
	checker = &update.Checker{API: srv.URL, Repo: brand.GitHubRepo, Client: srv.Client()}
	defer func() { checker = old }()
	cfgPath := filepath.Join(t.TempDir(), "c.conf")
	os.WriteFile(cfgPath, []byte("[updates]\nchannel = \"prerelease\"\n"), 0o644)
	oldPath := configmod.Path
	configmod.Path = cfgPath
	defer func() { configmod.Path = oldPath }()

	res, err := check(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/repos/ervisio/ervisio/releases" || res.Channel != "prerelease" {
		t.Fatalf("path %s channel %s", path, res.Channel)
	}
	if res.Latest == nil || res.Latest.Version != "99.0.0-rc.1" || res.Latest.Size != 1234 || !res.Newer || res.Latest.Notes != "# RC" {
		t.Fatalf("result %+v latest %+v", res, res.Latest)
	}
}

func TestStatusDevAndErrors(t *testing.T) {
	DaemonDev = true
	defer func() { DaemonDev = false }()
	s := status()
	if s.CanUpdate || s.Reason == "" || s.Current != brand.Version || s.Installed == nil {
		t.Fatalf("status %+v", s)
	}
	for err, code := range map[error]rpc.Code{
		update.ErrBusy:         rpc.Conflict,
		update.ErrPackages:     rpc.Conflict,
		update.ErrUpToDate:     rpc.Conflict,
		update.ErrNoPrevious:   rpc.NotFound,
		update.ErrNotInstalled: rpc.Unavailable,
		errors.New("boom"):     rpc.Internal,
	} {
		if !rpc.IsCode(mapErr(err), code) {
			t.Errorf("%v -> %v, want %s", err, mapErr(err), code)
		}
	}
}

func TestManagedInstall(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "managed")
	os.WriteFile(marker, []byte("pacman\n"), 0o644)
	old := newUpdater
	newUpdater = func() *update.Updater {
		u := update.New(checker)
		u.Layout.Managed = marker
		return u
	}
	defer func() { newUpdater = old }()
	s := status()
	if s.ManagedBy != "pacman" || s.CanUpdate || !strings.Contains(s.Reason, "pacman") {
		t.Fatalf("status %+v", s)
	}
	if err := mapErr(update.ErrManaged); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("ErrManaged -> %v", err)
	}
	if _, err := rollback(context.Background(), ""); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatalf("rollback on a managed install: %v", err)
	}
}
