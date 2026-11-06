package backup

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// CI alone opts into fresh disposable PostgreSQL/MariaDB services on these
// non-default loopback ports. This test never reads a configured production DB.
func TestDisposableDatabaseDumpAndRestore(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("ERVISIO_BACKUP_TEST_DATABASES") != "1" {
		t.Skip("opt in only for disposable CI database services")
	}
	if p := os.Getenv("ERVISIO_BACKUP_TEST_POSTGRES_PORT"); p != "" && p != "15432" {
		t.Fatal("PostgreSQL fixture must use isolated port15432")
	}
	if p := os.Getenv("ERVISIO_BACKUP_TEST_MYSQL_PORT"); p != "" && p != "13306" {
		t.Fatal("MySQL fixture must use isolated port13306")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	password := "backup-fixture-only"
	database := "ervisio_backup_fixture"
	t.Run("PostgreSQL", func(t *testing.T) {
		for _, program := range []string{"psql", "pg_dump", "pg_restore"} {
			if _, err := exec.LookPath(program); err != nil {
				t.Fatalf("required disposable-test client absent: %s", program)
			}
		}
		dir := t.TempDir()
		pgpass := filepath.Join(dir, "pgpass")
		os.WriteFile(pgpass, []byte("127.0.0.1:15432:"+database+":backup:"+password+"\n"), 0600)
		env := append(cleanDBEnvironment(), "PGPASSFILE="+pgpass, "PGCONNECT_TIMEOUT=10")
		base := []string{"--host", "127.0.0.1", "--port", "15432", "--username", "backup", "--dbname", database}
		query := func(sql string) string {
			t.Helper()
			argv := append(append([]string{}, base...), "--no-psqlrc", "--no-align", "--tuples-only", "--set", "ON_ERROR_STOP=1", "--command", sql)
			return fixtureClient(t, ctx, "psql", argv, env, nil)
		}
		table := "ervisio_backup_probe_" + newID()[:12]
		query("CREATE TABLE public." + table + " (value text); INSERT INTO public." + table + " VALUES ('fixture-expected');")
		defer func() {
			cmd := exec.Command("psql", append(append([]string{}, base...), "--no-psqlrc", "--command", "DROP TABLE IF EXISTS public."+table)...)
			cmd.Env = env
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			_ = cmd.Run()
		}()
		dump := filepath.Join(dir, "postgres.dump")
		if err := dumpDatabase(ctx, Database{Kind: "postgres", Name: database, Host: "127.0.0.1", Port: 15432, User: "backup", Password: &password}, dump, &budget{limit: 16 << 20, dir: dir}); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(dump)
		if err != nil || !strings.HasPrefix(string(b), "PGDMP") {
			t.Fatal("real pg_dump did not produce a custom-format archive")
		}
		query("DROP TABLE public." + table)
		argv := append(append([]string{}, base...), "--no-owner", "--no-privileges", "--table", table, dump)
		fixtureClient(t, ctx, "pg_restore", argv, env, nil)
		if out := query("SELECT value FROM public." + table); strings.TrimSpace(out) != "fixture-expected" {
			t.Fatal("PostgreSQL restored data did not match fixture")
		}
	})
	t.Run("MariaDB", func(t *testing.T) {
		client := "mariadb"
		if _, err := exec.LookPath(client); err != nil {
			client = "mysql"
		}
		if _, err := exec.LookPath(client); err != nil {
			t.Fatal("required MariaDB/MySQL fixture client absent")
		}
		dir := t.TempDir()
		config := filepath.Join(dir, "client.cnf")
		os.WriteFile(config, []byte("[client]\nprotocol=tcp\nhost=127.0.0.1\nport=13306\nuser=backup\npassword="+password+"\n"), 0600)
		argv := []string{"--defaults-file=" + config, "--batch", "--skip-column-names", database}
		env := cleanDBEnvironment()
		query := func(sql string) string { return fixtureClient(t, ctx, client, argv, env, strings.NewReader(sql)) }
		table := "ervisio_backup_probe_" + newID()[:12]
		query("CREATE TABLE " + table + " (value varchar(64)); INSERT INTO " + table + " VALUES ('fixture-expected');")
		defer func() {
			cmd := exec.Command(client, argv...)
			cmd.Env = env
			cmd.Stdin = strings.NewReader("DROP TABLE IF EXISTS " + table)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			_ = cmd.Run()
		}()
		dump := filepath.Join(dir, "mysql.sql")
		if err := dumpDatabase(ctx, Database{Kind: "mysql", Name: database, Host: "127.0.0.1", Port: 13306, User: "backup", Password: &password}, dump, &budget{limit: 16 << 20, dir: dir}); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(dump)
		if err != nil || !strings.Contains(string(b), "CREATE TABLE") || !strings.Contains(string(b), "fixture-expected") {
			t.Fatal("real MySQL/MariaDB dump lacks fixture schema/data")
		}
		query("DROP TABLE " + table)
		fixtureClient(t, ctx, client, argv, env, strings.NewReader(string(b)))
		if out := query("SELECT value FROM " + table); strings.TrimSpace(out) != "fixture-expected" {
			t.Fatal("MariaDB restored data did not match fixture")
		}
	})
}

func fixtureClient(t *testing.T, ctx context.Context, program string, argv, env []string, stdin io.Reader) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, program, argv...)
	cmd.Env = env
	cmd.Stdin = stdin
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("disposable fixture %s failed (stderr redacted): %v", program, err)
	}
	return out.String()
}
