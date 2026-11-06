package remote

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// BackupConnections lists only account-owned SFTP/SMB metadata. It never returns
// passwords, private keys or the ciphertext/key files used by the remote store.
func BackupConnections() ([]View, error) {
	connections, err := (&store{}).list()
	if err != nil {
		return nil, err
	}
	views := []View{}
	for _, connection := range connections {
		if connection.Kind == KindSFTP || connection.Kind == KindSMB {
			views = append(views, connection.view())
		}
	}
	return views, nil
}

// WithBackupConnection lends a dedicated, cancellable connection to the backup
// helper running as the account that owns its saved remote. It never borrows a
// live file-manager connection or accepts credentials supplied by a plugin.
func WithBackupConnection(parent context.Context, id string, run func(context.Context, FS) error) error {
	if !validID(id) {
		return rpc.Errorf(rpc.Invalid, "Choose a saved SFTP or SMB connection.")
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Hour)
	defer cancel()
	spec, err := (&store{}).get(id)
	if err != nil {
		return err
	}
	if spec.Kind != KindSFTP && spec.Kind != KindSMB {
		return rpc.Errorf(rpc.Invalid, "Backups support saved SFTP and SMB connections.")
	}
	f, err := dialFS(ctx, spec)
	if err != nil {
		return remoteErr(err)
	}
	var once sync.Once
	closeOwned := func() {
		once.Do(func() {
			switch connection := f.(type) {
			case *sftpFS:
				_ = connection.ssh.Close()
			case *smbFS:
				_ = connection.conn.Close()
			}
			_ = f.Close()
		})
	}
	stop := context.AfterFunc(ctx, closeOwned)
	defer stop()
	defer closeOwned()
	err = run(ctx, backupFS{FS: f})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return remoteErr(err)
}

// backupFS restricts new SFTP artifacts before the first byte of database data
// is written. SMB permissions remain controlled by the server's share ACLs.
type backupFS struct{ FS }

func (f backupFS) Close() error {
	switch connection := f.FS.(type) {
	case *sftpFS:
		_ = connection.ssh.Close()
	case *smbFS:
		_ = connection.conn.Close()
	}
	return f.FS.Close()
}

func (f backupFS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	if sftp, ok := f.FS.(*sftpFS); ok {
		file, err := sftp.c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return nil, err
		}
		if err = file.Chmod(0600); err != nil {
			_ = file.Close()
			_ = sftp.c.Remove(p)
			return nil, err
		}
		return file, nil
	}
	return f.FS.Create(ctx, p, size)
}

func (f backupFS) Mkdir(ctx context.Context, p string) error {
	if sftp, ok := f.FS.(*sftpFS); ok {
		if err := sftp.Mkdir(ctx, p); err != nil {
			return err
		}
		return sftp.c.Chmod(p, 0700)
	}
	return f.FS.Mkdir(ctx, p)
}

// SecureBackupDirectory restricts a previously verified, job-owned SFTP
// namespace. The user's selected parent and SMB ACLs remain server controlled.
func SecureBackupDirectory(ctx context.Context, f FS, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if wrapper, ok := f.(backupFS); ok {
		if sftp, ok := wrapper.FS.(*sftpFS); ok {
			return sftp.c.Chmod(p, 0700)
		}
	}
	return nil
}
