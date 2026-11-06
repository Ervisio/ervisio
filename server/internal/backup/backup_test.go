package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCalendarMonthAnchorAndBounds(t *testing.T) {
	jan := time.Date(2027, 1, 31, 10, 30, 0, 0, time.UTC)
	p := Period{1, "months"}
	feb := p.Next(jan, 31)
	march := p.Next(feb, 31)
	if feb.Day() != 28 || feb.Month() != 2 || march.Day() != 31 || march.Month() != 3 {
		t.Fatalf("lost anchor: %v %v", feb, march)
	}
	if (Period{0, "minutes"}).valid() || (Period{13, "months"}).valid() || (Period{1, "seconds"}).valid() {
		t.Fatal("invalid period accepted")
	}
}

func TestMonthlyLateFirstTickAndDowntimeDoNotSkipBoundary(t *testing.T) {
	c := fixturePlan()
	c.Frequency = Period{1, "months"}
	c.AnchorDay = 31
	c.NextDue = "2027-01-31T14:30:00Z"
	first := nextDue(c, time.Date(2027, 2, 1, 14, 30, 0, 0, time.UTC))
	if first.Format(time.RFC3339) != "2027-02-28T14:30:00Z" {
		t.Fatalf("late initial tick skipped February: %v", first)
	}
	c.NextDue = first.Format(time.RFC3339)
	for _, now := range []time.Time{time.Date(2027, 2, 28, 14, 30, 0, 0, time.UTC), time.Date(2027, 3, 1, 14, 30, 0, 0, time.UTC)} {
		next := nextDue(c, now)
		if next.Format(time.RFC3339) != "2027-03-31T14:30:00Z" {
			t.Fatalf("late tick lost original boundary: %v", next)
		}
	}
}

func TestValidationRejectsTraversalAndDBConnectionInjection(t *testing.T) {
	for _, value := range []string{"/backups/../customer", `\\server\share`, "/bad\npath"} {
		c := fixturePlan()
		c.Destination.Path = value
		if validate(c) == nil {
			t.Fatalf("accepted destination %q", value)
		}
	}
	for _, name := range []string{"--host=evil", "postgresql://user:pass@evil/db", "name\nother"} {
		c := fixturePlan()
		c.Mode = "databases"
		c.Databases = []Database{{Kind: "postgres", Name: name, Host: "localhost", User: "app"}}
		if validate(c) == nil {
			t.Fatalf("accepted database injection %q", name)
		}
	}
}

func TestArchiveExcludesAndDoesNotFollowLinks(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	spool := filepath.Join(dir, "spool")
	os.Mkdir(source, 0700)
	os.Mkdir(spool, 0700)
	os.WriteFile(filepath.Join(source, "keep.txt"), []byte("expected-content"), 0600)
	os.WriteFile(filepath.Join(source, "skip.log"), []byte("excluded"), 0600)
	outside := filepath.Join(dir, "secret")
	os.WriteFile(outside, []byte("must-not-follow"), 0600)
	linked := os.Symlink(outside, filepath.Join(source, "link.txt")) == nil
	c := fixturePlan()
	c.Folders = []string{source}
	c.Excludes = []string{"*.log"}
	p, _, info, err := buildArchive(context.Background(), c, newID(), spool, filepath.Join(dir, "unrelated"), &budget{limit: 4 << 20, dir: spool})
	if err != nil {
		t.Fatal(err)
	}
	if info.Files != 1 || info.Excluded != 1 || linked && info.SkippedLinks != 1 {
		t.Fatalf("unexpected stats: %+v", info)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		if strings.HasSuffix(h.Name, "/keep.txt") {
			found = string(b) == "expected-content"
		}
		if bytes.Contains(b, []byte("must-not-follow")) || strings.Contains(h.Name, "skip.log") || h.Typeflag == tar.TypeSymlink {
			t.Fatal("excluded file or symlink target archived")
		}
	}
	if !found {
		t.Fatal("source file missing")
	}
}

func TestSourceQuotaFailsBeforePublishing(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	spool := filepath.Join(dir, "spool")
	os.Mkdir(source, 0700)
	os.Mkdir(spool, 0700)
	os.WriteFile(filepath.Join(source, "large"), bytes.Repeat([]byte("x"), 2<<20), 0600)
	c := fixturePlan()
	c.Folders = []string{source}
	if _, _, _, err := buildArchive(context.Background(), c, newID(), spool, "unused", &budget{limit: 4 << 20, dir: spool}); err == nil {
		t.Fatal("source quota not enforced")
	}
}

func TestSourceParentNeverArchivesPrivateStoreOrActiveSpool(t *testing.T) {
	home := t.TempDir()
	own := filepath.Join(home, ".config", "ervisio", "plugins", "backup")
	spool := filepath.Join(own, "spool-fixture")
	if err := os.MkdirAll(spool, 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(own, "backup.key"), []byte("never-archive-this-private-key"), 0600)
	os.WriteFile(filepath.Join(home, "application.txt"), []byte("application-data"), 0600)
	c := fixturePlan()
	c.Folders = []string{home}
	archive, _, info, err := buildArchive(context.Background(), c, newID(), spool, own, &budget{limit: 4 << 20, dir: spool})
	if err != nil {
		t.Fatal(err)
	}
	if info.Files != 1 {
		t.Fatalf("private subtree included or recursed: %+v", info)
	}
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		if strings.Contains(h.Name, "backup.key") || strings.Contains(h.Name, "spool-fixture") || bytes.Contains(b, []byte("never-archive-this-private-key")) {
			t.Fatal("own private store/spool included")
		}
	}
}

func TestArchiveOpenRejectsReplacedAncestorSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux dirfd nofollow traversal")
	}
	base := t.TempDir()
	os.Mkdir(filepath.Join(base, "folder"), 0700)
	os.Mkdir(filepath.Join(base, "private"), 0700)
	os.WriteFile(filepath.Join(base, "folder", "key"), []byte("public"), 0600)
	os.WriteFile(filepath.Join(base, "private", "key"), []byte("private-key"), 0600)
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err = root.Lstat("folder/key"); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(base, "folder"), filepath.Join(base, "old-folder"))
	if err = os.Symlink("private", filepath.Join(base, "folder")); err != nil {
		t.Fatal(err)
	}
	if f, err := openArchiveRead(root, "folder/key", false); err == nil {
		f.Close()
		t.Fatal("replaced ancestor symlink was followed")
	}
}

func TestSourceRootIdentityMustMatchInitialDirectory(t *testing.T) {
	first := t.TempDir()
	replacement := t.TempDir()
	expected, err := os.Lstat(first)
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(replacement)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = verifySourceRoot(root, expected); err == nil {
		t.Fatal("source root replacement was accepted")
	}
}

func TestStoreSealsPasswordsAndBlankPreservesSecret(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("private POSIX store and cross-process flock run on Linux")
	}
	s := store{dir: t.TempDir()}
	if err := s.init(); err != nil {
		t.Fatal(err)
	}
	secret := "extremely-private-db-password"
	c := fixturePlan()
	c.ID = ""
	c.Mode = "databases"
	c.Databases = []Database{{Kind: "postgres", Name: "app", Host: "localhost", User: "backup", Password: &secret}}
	if err := s.save(c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "state", c.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("plaintext DB password in persistent config")
	}
	items, err := s.list()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(items)
	if bytes.Contains(b, []byte(secret)) || bytes.Contains(b, []byte("v1:")) {
		t.Fatal("secret returned by list")
	}
	if !items[0].Databases[0].HasPassword {
		t.Fatal("missing secret-presence indicator")
	}
	blank := ""
	items[0].Databases[0].Password = &blank
	if err = s.save(&items[0]); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.load(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Databases[0].Password == nil || *loaded.Databases[0].Password != secret {
		t.Fatal("blank edit lost saved secret")
	}
	items[0].Databases[0].ClearPassword = true
	if err = s.save(&items[0]); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.load(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Databases[0].Password != nil {
		t.Fatal("explicit clear did not remove secret")
	}
}

func TestInboxRefusesLinksWithoutDeletingTheirTarget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux nofollow boundary")
	}
	s := store{dir: t.TempDir()}
	if err := s.init(); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	os.WriteFile(outside, []byte(`{"sensitive":true}`), 0600)
	token := newID()
	if err := os.Symlink(outside, filepath.Join(s.dir, "inbox", token+".json")); err != nil {
		t.Fatal(err)
	}
	if err := s.inbox(token, new(Config)); err == nil {
		t.Fatal("inbox link followed")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("external target changed")
	}
}

func TestCLIInvalidInputUsesStructuredErrorWithoutProfileAccess(t *testing.T) {
	var out bytes.Buffer
	e := &engine{store: store{dir: filepath.Join(t.TempDir(), "must-not-create")}}
	if runCLI(context.Background(), []string{"run", "../../bad"}, &out, e) != 1 {
		t.Fatal("invalid ID succeeded")
	}
	var r response
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.OK || r.Error == nil || r.Error.Code != "backup_failed" {
		t.Fatalf("unexpected result: %s", out.Bytes())
	}
	if _, err := os.Stat(e.store.dir); !os.IsNotExist(err) {
		t.Fatal("invalid CLI touched the account profile")
	}
}
