//go:build linux

package maintenance

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfg "github.com/ervisio/ervisio/server/internal/config"
)

type fakeService struct {
	stop          func() error
	start         func() error
	stops, starts int
}

func (s *fakeService) Stop(context.Context) error {
	s.stops++
	if s.stop != nil {
		return s.stop()
	}
	return nil
}
func (s *fakeService) Start(context.Context) error {
	s.starts++
	if s.start != nil {
		return s.start()
	}
	return nil
}
func fixture(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home", "alice")
	o := Options{ConfigPath: filepath.Join(root, "etc", "ervisio", "ervisio.conf"), StateDir: filepath.Join(root, "var", "lib", "ervisio"), AIStateDir: filepath.Join(root, "var", "lib", "ervisio-ai"), CacheDir: filepath.Join(root, "var", "cache", "ervisio"), BackupDir: filepath.Join(root, "backups", "factory"), CurrentURL: "https://console.example:9443", Homes: []string{home}, PreserveAccess: true, Operator: "alice"}
	for _, path := range []string{o.ConfigPath, o.StateDir, o.AIStateDir, o.CacheDir, o.BackupDir, home} {
		relative, e := filepath.Rel(root, path)
		if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			t.Fatal("fixture path escaped isolated temporary root")
		}
	}
	for _, dir := range []string{filepath.Dir(o.ConfigPath), o.StateDir, o.AIStateDir, o.CacheDir, filepath.Join(home, ".config", "ervisio"), filepath.Join(home, ".local", "share", "ervisio-ai")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	current := cfg.Default()
	current.Listen = "127.0.0.1:9443"
	current.TLS.Mode = "custom"
	current.TLS.Cert = "/trusted/cert.pem"
	current.TLS.Key = "/trusted/key.pem"
	current.Auth.AllowUsers = []string{"alice"}
	current.UI.DefaultShell = "ervisios"
	current.Plugins.AllowUnsigned = true
	if err := cfg.Save(o.ConfigPath, current); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(o.StateDir, "plugins", "ai", "manifest.json"), "plugin-code")
	write(t, filepath.Join(o.StateDir, "updates", "last.json"), "update-metadata")
	write(t, filepath.Join(o.StateDir, "plugins-state.json"), "plugin-settings")
	write(t, filepath.Join(home, ".config", "ervisio", "plugins", "ai", "settings.json"), "PRIVATE-API-KEY")
	write(t, filepath.Join(home, ".config", "ervisio", "plugins", "ai", "chats", "chat.json"), "PRIVATE-CHAT")
	write(t, filepath.Join(home, ".local", "share", "ervisio-ai", "providers.json"), "PRIVATE-PROVIDER")
	write(t, filepath.Join(home, ".config", "linuxadmin", "prefs.json"), "LEGACY-PREFERENCES")
	write(t, filepath.Join(o.StateDir, "audit", "events.jsonl"), "OPERATIONAL-AUDIT")
	return o
}
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestBackupBeforeMutationAndCompleteResetPreservesCodeAndAccess(t *testing.T) {
	o := fixture(t)
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{stop: func() error {
		files, err := filepath.Glob(filepath.Join(o.BackupDir, "*.tar.gz"))
		if err != nil || len(files) != 1 {
			return errors.New("service stopped before backup completed")
		}
		return nil
	}}
	result, err := Apply(context.Background(), p, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.stops != 1 || s.starts != 1 {
		t.Fatalf("unexpected service lifecycle: %+v", s)
	}
	current, _, _, err := cfg.Load(o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if current.Listen != "127.0.0.1:9443" || current.TLS.Key != "/trusted/key.pem" || len(current.Auth.AllowUsers) != 1 {
		t.Fatal("connection/auth settings changed")
	}
	if current.UI.DefaultShell != "classic" || current.Plugins.AllowUnsigned {
		t.Fatal("non-access settings not reset")
	}
	for _, path := range []string{filepath.Join(o.StateDir, "plugins", "ai", "manifest.json"), filepath.Join(o.StateDir, "updates", "last.json")} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(filepath.Join(o.Homes[0], ".config", "ervisio")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("user keys/chats still present")
	}
	if _, err = os.Stat(filepath.Join(o.Homes[0], ".config", "linuxadmin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy profile could repopulate clean preferences")
	}
	if _, err = os.Stat(filepath.Join(o.StateDir, ".factory-reset")); err != nil {
		t.Fatal("global migration suppression marker absent")
	}
	if body, e := os.ReadFile(filepath.Join(o.StateDir, "audit", "events.jsonl")); e != nil || string(body) != "OPERATIONAL-AUDIT" {
		t.Fatal("operational audit was removed")
	}
	f, err := os.Open(result.Backup)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, _ := f.Stat()
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup is not private")
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		header, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasSuffix(header.Name, "plugins/ai/settings.json") {
			body, _ := io.ReadAll(tr)
			found = string(body) == "PRIVATE-API-KEY"
		}
	}
	if !found {
		t.Fatal("private data not preserved in backup")
	}
}
func TestRestartFailureRestoresEveryStagedPath(t *testing.T) {
	o := fixture(t)
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	s.start = func() error {
		if s.starts == 1 {
			return errors.New("fixture restart failure")
		}
		return nil
	}
	if _, err = Apply(context.Background(), p, s, nil); err == nil {
		t.Fatal("expected restart failure")
	}
	body, err := os.ReadFile(filepath.Join(o.Homes[0], ".config", "ervisio", "plugins", "ai", "settings.json"))
	if err != nil || string(body) != "PRIVATE-API-KEY" {
		t.Fatal("rollback did not restore keys")
	}
	old, _, _, err := cfg.Load(o.ConfigPath)
	if err != nil || old.UI.DefaultShell != "ervisios" || !old.Plugins.AllowUnsigned {
		t.Fatal("rollback did not restore config")
	}
	if s.starts != 2 {
		t.Fatal("old service was not restarted")
	}
}
func TestChangedPlanIsRejectedBeforeBackupOrStopping(t *testing.T) {
	o := fixture(t)
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(o.StateDir, "new-file"), "changed")
	s := &fakeService{}
	if _, err = Apply(context.Background(), p, s, nil); err == nil {
		t.Fatal("changed inventory accepted")
	}
	if s.stops != 0 {
		t.Fatal("stopped service for a stale plan")
	}
}
func TestSymlinkTargetsAreNeverFollowedAndAncestorLinksRefusePlan(t *testing.T) {
	o := fixture(t)
	outside := filepath.Join(t.TempDir(), "keep.txt")
	write(t, outside, "KEEP")
	link := filepath.Join(o.Homes[0], ".config", "ervisio", "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Apply(context.Background(), p, &fakeService{}, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outside)
	if err != nil || string(body) != "KEEP" {
		t.Fatal("external symlink target changed")
	}
	o2 := fixture(t)
	config := filepath.Join(o2.Homes[0], ".config")
	if err = os.Rename(config, config+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(config+"-moved", config); err != nil {
		t.Fatal(err)
	}
	if _, err = Preview(context.Background(), o2); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
}
func TestProtectedSwapAndMountSubtreesStayInPlace(t *testing.T) {
	o := fixture(t)
	swap := filepath.Join(o.StateDir, "recovery.swap")
	mount := filepath.Join(o.Homes[0], ".config", "ervisio", "data", "mounted")
	write(t, swap, "SWAP")
	write(t, filepath.Join(mount, "world"), "WORLD")
	write(t, filepath.Join(filepath.Dir(mount), "settings"), "DELETE")
	previous := systemProtected
	systemProtected = func() ([]string, error) { return []string{swap, mount}, nil }
	defer func() { systemProtected = previous }()
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Apply(context.Background(), p, &fakeService{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{swap, filepath.Join(mount, "world")} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(mount), "settings")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ordinary sibling was not reset")
	}
}
func TestBackupCannotBeInsideResetRoots(t *testing.T) {
	o := fixture(t)
	o.BackupDir = filepath.Join(o.StateDir, "backup")
	if _, err := Preview(context.Background(), o); err == nil {
		t.Fatal("recursive backup destination accepted")
	}
}

func TestConnectionDefaultsDoNotEnableRootAndPhysicalCustomTLSKeysArePreserved(t *testing.T) {
	o := fixture(t)
	key := filepath.Join(o.Homes[0], ".config", "ervisio", "certificates", "key.pem")
	cert := filepath.Join(filepath.Dir(key), "cert.pem")
	write(t, key, "PRIVATE-TLS-KEY")
	write(t, cert, "PUBLIC-CERT")
	current, _, _, err := cfg.Load(o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	current.TLS.Key = key
	current.TLS.Cert = cert
	if err = cfg.Save(o.ConfigPath, current); err != nil {
		t.Fatal(err)
	}
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Apply(context.Background(), p, &fakeService{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{key, cert} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal("physical TLS material removed")
		}
	}
	o = fixture(t)
	o.PreserveAccess = false
	o.Operator = "root"
	p, err = Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if p.NextURL != "https://console.example:9090" {
		t.Fatal("new address not previewed")
	}
	if _, err = Apply(context.Background(), p, &fakeService{}, nil); err != nil {
		t.Fatal(err)
	}
	fresh, _, _, err := cfg.Load(o.ConfigPath)
	if err != nil || fresh.AllowRoot {
		t.Fatal("factory defaults relaxed root access")
	}
}

func TestQuarantineDoesNotDeleteUnbackedNestedFiles(t *testing.T) {
	o := fixture(t)
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	foreign := ""
	s := &fakeService{start: func() error {
		containers, _ := filepath.Glob(filepath.Join(o.Homes[0], ".config", ".ervisio-reset-*"))
		for _, container := range containers {
			candidate := filepath.Join(container, "data", "plugins", "ai", "settings.json")
			if _, e := os.Stat(candidate); e == nil {
				foreign = filepath.Join(container, "data", "unbacked.txt")
				write(t, foreign, "UNBACKED")
				break
			}
		}
		return nil
	}}
	if _, err = Apply(context.Background(), p, s, nil); err == nil {
		t.Fatal("unbacked staged files were not detected")
	}
	body, e := os.ReadFile(foreign)
	if e != nil || string(body) != "UNBACKED" {
		t.Fatal("unbacked replacement was deleted")
	}
}
func TestRollbackPreservesDataCreatedByFailedNewDaemon(t *testing.T) {
	o := fixture(t)
	p, err := Preview(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	s.start = func() error {
		if s.starts == 1 {
			write(t, filepath.Join(o.Homes[0], ".config", "ervisio", "new-document.txt"), "NEW-DATA")
			return errors.New("new daemon health failed")
		}
		return nil
	}
	if _, err = Apply(context.Background(), p, s, nil); err == nil {
		t.Fatal("expected restart failure")
	}
	if s.stops != 2 {
		t.Fatal("new daemon was not stopped before rollback")
	}
	old, err := os.ReadFile(filepath.Join(o.Homes[0], ".config", "ervisio", "plugins", "ai", "settings.json"))
	if err != nil || string(old) != "PRIVATE-API-KEY" {
		t.Fatal("old data not restored")
	}
	found := false
	containers, _ := filepath.Glob(filepath.Join(o.Homes[0], ".config", ".ervisio-reset-*"))
	for _, container := range containers {
		body, e := os.ReadFile(filepath.Join(container, "during-reset", "new-document.txt"))
		if e == nil && string(body) == "NEW-DATA" {
			found = true
		}
	}
	if !found {
		t.Fatal("unbacked new data was deleted during rollback")
	}
}
func TestMountedConfigurationRootRefusesAllChanges(t *testing.T) {
	o := fixture(t)
	previous := systemProtected
	systemProtected = func() ([]string, error) { return []string{filepath.Dir(o.ConfigPath)}, nil }
	defer func() { systemProtected = previous }()
	if _, err := Preview(context.Background(), o); err == nil {
		t.Fatal("mounted config was skipped but could still be overwritten")
	}
}
