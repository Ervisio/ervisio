package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("ERVISIO_BACKUP_FAKE_CLIENT") == "1" {
		b, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(os.Getenv("ERVISIO_BACKUP_ARGV_LOG"), b, 0600)
		if os.Getenv("ERVISIO_BACKUP_FAKE_FAILURE") == "1" {
			fmt.Fprintln(os.Stderr, "password=fixture-very-secret")
			os.Exit(3)
		}
		fmt.Print("fixture-database-dump\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDatabaseClientNeverReceivesPasswordInArgvOrError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux client process fixture")
	}
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"pg_dump", "mysqldump"} {
		if err = os.Symlink(executable, filepath.Join(dir, client)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("ERVISIO_BACKUP_FAKE_CLIENT", "1")
	log := filepath.Join(dir, "argv.json")
	t.Setenv("ERVISIO_BACKUP_ARGV_LOG", log)
	password := "fixture-very-secret"
	for _, kind := range []string{"postgres", "mysql"} {
		db := Database{Kind: kind, Name: "application", Host: "localhost", Port: 5432, User: "backup", Password: &password}
		target := filepath.Join(dir, "dump-"+kind)
		if err = dumpDatabase(context.Background(), db, target, &budget{limit: 4 << 20, dir: dir}); err != nil {
			t.Fatal(err)
		}
		argv, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(argv, []byte(password)) {
			t.Fatal("password passed in argv")
		}
	}
	t.Setenv("ERVISIO_BACKUP_FAKE_FAILURE", "1")
	err = dumpDatabase(context.Background(), Database{Kind: "postgres", Name: "application", Host: "localhost", Port: 5432, User: "backup", Password: &password}, filepath.Join(dir, "failed"), &budget{limit: 4 << 20, dir: dir})
	if err == nil || strings.Contains(err.Error(), password) {
		t.Fatal("client error leaked raw stderr or failure was ignored")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".db-client-") {
			t.Fatal("private client password file not removed")
		}
	}
}

func TestSQLiteOnlineBackupUsesRealConsistentAPI(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Python stdlib fixture")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python3 absent")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source with spaces.sqlite")
	target := filepath.Join(dir, "snapshot.sqlite")
	create := exec.Command(python, "-I", "-c", `import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.execute('CREATE TABLE fixture (value TEXT)'); c.execute("INSERT INTO fixture VALUES ('expected')"); c.commit(); c.close()`, source)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v %s", err, out)
	}
	if err = dumpSQLite(context.Background(), Database{Kind: "sqlite", Name: "fixture", Path: source}, target, &budget{limit: 8 << 20, dir: dir}); err != nil {
		t.Fatal(err)
	}
	verify := exec.Command(python, "-I", "-c", `import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); assert c.execute('SELECT value FROM fixture').fetchone()[0]=='expected'; assert c.execute('PRAGMA integrity_check').fetchone()[0]=='ok'`, target)
	if out, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("verify snapshot: %v %s", err, out)
	}
}
