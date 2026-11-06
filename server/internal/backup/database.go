package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// argv contains only validated public connection metadata. Passwords are passed
// through a private client option file; stderr is deliberately never surfaced.
func dumpDatabase(ctx context.Context, db Database, target string, b *budget) error {
	if db.Kind == "sqlite" {
		return dumpSQLite(ctx, db, target, b)
	}
	program := "pg_dump"
	if db.Kind == "mysql" {
		program = "mysqldump"
		if _, err := exec.LookPath(program); err != nil {
			program = "mariadb-dump"
		}
	}
	resolved, err := exec.LookPath(program)
	if err != nil {
		return fmt.Errorf("The %s client is not installed. Install the database client in Software and retry.", program)
	}
	secret, err := os.CreateTemp(filepath.Dir(target), ".db-client-")
	if err != nil {
		return err
	}
	secretPath := secret.Name()
	defer os.Remove(secretPath)
	if err = secret.Chmod(0600); err != nil {
		secret.Close()
		return err
	}
	password := ""
	if db.Password != nil {
		password = *db.Password
	}
	var content string
	var argv []string
	env := cleanDBEnvironment()
	if db.Kind == "postgres" {
		escape := func(s string) string { return strings.NewReplacer(`\`, `\\`, ":", `\:`).Replace(s) }
		content = strings.Join([]string{escape(db.Host), strconv.Itoa(db.Port), escape(db.Name), escape(db.User), escape(password)}, ":") + "\n"
		argv = []string{"--format=custom", "--no-password", "--host", db.Host, "--port", strconv.Itoa(db.Port), "--username", db.User, "--dbname", db.Name}
		env = append(env, "PGPASSFILE="+secretPath, "PGCONNECT_TIMEOUT=15")
	} else {
		quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
		content = "[client]\nhost=" + quote(db.Host) + "\nport=" + strconv.Itoa(db.Port) + "\nuser=" + quote(db.User) + "\npassword=" + quote(password) + "\n"
		argv = []string{"--defaults-file=" + secretPath, "--single-transaction", "--quick", "--routines", "--events", "--triggers", "--hex-blob", "--databases", db.Name}
	}
	if _, err = io.WriteString(secret, content); err != nil {
		secret.Close()
		return err
	}
	if err = secret.Close(); err != nil {
		return err
	}
	f, err := openProtected(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, resolved, argv...)
	cmd.Env = env
	cmd.Stdout = budgetWriter{f, b}
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("The %s dump failed. Check database access, credentials, client/server compatibility and the backup size limit.", program)
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return errors.New("The database client returned an empty dump.")
	}
	return nil
}

func cleanDBEnvironment() []string {
	out := []string{}
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(name, "PG") || strings.HasPrefix(name, "MYSQL") || strings.HasPrefix(name, "MARIADB") {
			continue
		}
		out = append(out, item)
	}
	return out
}

// sqlite3's online backup API produces a consistent snapshot including WAL
// data. It never copies a live database file as if it were an ordinary file.
const sqliteBackupScript = `import os, sqlite3, sys, urllib.parse, shutil
source, target, maximum, reserve = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
os.umask(0o077)
uri = 'file:' + urllib.parse.quote(source, safe='/') + '?mode=ro'
with sqlite3.connect(uri, uri=True, timeout=15) as src:
    page = src.execute('PRAGMA page_size').fetchone()[0]
    if src.execute('PRAGMA page_count').fetchone()[0] * page > maximum:
        raise RuntimeError('size limit')
    with sqlite3.connect(target) as dst:
        def progress(status, remaining, total):
            if total * page > maximum or shutil.disk_usage(os.path.dirname(target)).free < reserve:
                raise RuntimeError('spool limit')
        src.backup(dst, pages=128, progress=progress, sleep=0.05)
        if dst.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
            raise RuntimeError('integrity check failed')
`

func dumpSQLite(ctx context.Context, db Database, target string, b *budget) error {
	python, err := exec.LookPath("python3")
	if err != nil {
		return errors.New("Python 3 is required for consistent SQLite backups. Install python3 in Software.")
	}
	fi, err := os.Lstat(db.Path)
	if err != nil || !fi.Mode().IsRegular() {
		return errors.New("The SQLite source must be an existing readable regular file, not a link.")
	}
	remaining := b.limit - b.used - (1 << 20)
	if remaining < 1<<20 {
		return errors.New("There is insufficient spool space for the SQLite snapshot.")
	}
	f, err := openProtected(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	f.Close()
	cmd := exec.CommandContext(ctx, python, "-I", "-c", sqliteBackupScript, db.Path, target, strconv.FormatInt(remaining, 10), strconv.FormatInt(reserveBytes, 10))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("The consistent SQLite snapshot failed. Check access, database integrity and available spool space.")
	}
	fi, err = os.Lstat(target)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 || fi.Size() > remaining {
		return errors.New("The SQLite snapshot is empty or exceeds the spool limit.")
	}
	b.used += fi.Size()
	return nil
}
