package remote

import (
	"context"
	"io"
	"net"
	"os"
	"path"
	"strings"

	"github.com/hirochachacha/go-smb2"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// smbFS is one SMB2/3 session with one mounted share.
type smbFS struct {
	conn  net.Conn
	s     *smb2.Session
	share *smb2.Share
}

func dialSMB(ctx context.Context, spec *Conn) (*smbFS, error) {
	conn, err := dialTCP(ctx, spec.address())
	if err != nil {
		return nil, err
	}
	user := spec.User
	if user == "" {
		user = "guest"
	}
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: user, Password: spec.secret(secPassword), Domain: spec.Domain}}
	dctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	s, err := d.DialContext(dctx, conn)
	if err != nil {
		_ = conn.Close()
		if strings.Contains(err.Error(), "LOGON_FAILURE") || strings.Contains(err.Error(), "ACCOUNT") || os.IsPermission(err) {
			return nil, rpc.Errorf(rpc.Forbidden, "The server refused the sign-in of %s. Check the user name, the password and the domain.", user)
		}
		return nil, rpc.Errorf(rpc.Unavailable, "The SMB session with %s could not be set up: %v", spec.address(), err)
	}
	sh, err := s.Mount(spec.Share)
	if err != nil {
		_ = s.Logoff()
		_ = conn.Close()
		switch {
		case strings.Contains(err.Error(), "BAD_NETWORK_NAME"), os.IsNotExist(err):
			return nil, rpc.Errorf(rpc.NotFound, "The server has no shared folder called %q.", spec.Share)
		case os.IsPermission(err):
			return nil, rpc.Errorf(rpc.Forbidden, "%s may not open the shared folder %q.", user, spec.Share)
		}
		return nil, rpc.Errorf(rpc.Unavailable, "The shared folder %q could not be opened: %v", spec.Share, err)
	}
	return &smbFS{conn: conn, s: s, share: sh}, nil
}

// smbPath turns "/a/b" into `a\b` (relative to the share).
func smbPath(p string) string {
	return strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", `\`)
}

func (f *smbFS) sh(ctx context.Context) *smb2.Share { return f.share.WithContext(ctx) }

func (f *smbFS) List(ctx context.Context, dir string) ([]Entry, error) {
	fis, err := f.sh(ctx).ReadDir(smbPath(dir))
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(fis))
	for _, fi := range fis {
		if fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		out = append(out, infoEntry(dir, fi))
	}
	return out, nil
}

func (f *smbFS) Stat(ctx context.Context, p string) (Entry, error) {
	if p == "/" {
		return dirEntry("/"), nil
	}
	fi, err := f.sh(ctx).Lstat(smbPath(p))
	if err != nil {
		return Entry{}, err
	}
	e := infoEntry(path.Dir(p), fi)
	e.Name, e.Path = path.Base(p), p
	if e.Type == "symlink" {
		if ti, err := f.sh(ctx).Stat(smbPath(p)); err == nil {
			e.TargetType = infoEntry(path.Dir(p), ti).Type
		}
	}
	return e, nil
}

func (f *smbFS) Mkdir(ctx context.Context, p string) error {
	return f.sh(ctx).Mkdir(smbPath(p), 0o755)
}

func (f *smbFS) Rename(ctx context.Context, from, to string) error {
	return f.sh(ctx).Rename(smbPath(from), smbPath(to))
}

func (f *smbFS) Remove(ctx context.Context, p string, dir bool) error {
	return f.sh(ctx).Remove(smbPath(p))
}

func (f *smbFS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	// No WithContext: the file outlives this call (it is read by the stream).
	return f.share.Open(smbPath(p))
}

func (f *smbFS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	return f.share.OpenFile(smbPath(p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}

func (f *smbFS) Close() error {
	_ = f.share.Umount()
	_ = f.s.Logoff()
	return f.conn.Close()
}
