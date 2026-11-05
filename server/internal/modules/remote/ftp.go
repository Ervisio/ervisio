package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/textproto"
	"os"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// ftpFS is one FTP control connection. It runs one command at a time, so
// the pool hands it to one caller at a time (and opens a few in parallel).
type ftpFS struct {
	c *ftp.ServerConn
}

func dialFTP(ctx context.Context, spec *Conn) (*ftpFS, error) {
	opts := []ftp.DialOption{ftp.DialWithContext(ctx), ftp.DialWithTimeout(dialTimeout), ftp.DialWithLocation(time.UTC)}
	tcfg := &tls.Config{ServerName: spec.Host, InsecureSkipVerify: spec.Insecure, MinVersion: tls.VersionTLS12}
	switch spec.TLS {
	case "implicit":
		opts = append(opts, ftp.DialWithTLS(tcfg))
	case "none":
	default:
		opts = append(opts, ftp.DialWithExplicitTLS(tcfg))
	}
	c, err := ftp.Dial(spec.address(), opts...)
	if err != nil {
		return nil, ftpDialErr(spec, err)
	}
	user, pw := spec.User, spec.secret(secPassword)
	if user == "" {
		user, pw = "anonymous", "anonymous@"
	}
	if err := c.Login(user, pw); err != nil {
		_ = c.Quit()
		var te *textproto.Error
		if errors.As(err, &te) && (te.Code == 530 || te.Code == 430) {
			return nil, rpc.Errorf(rpc.Forbidden, "The server refused the sign-in of %s: %s", user, te.Msg)
		}
		return nil, rpc.Errorf(rpc.Unavailable, "FTP sign-in failed: %v", err)
	}
	return &ftpFS{c: c}, nil
}

func ftpDialErr(spec *Conn, err error) error {
	var cert *tls.CertificateVerificationError
	switch {
	case errors.As(err, &cert):
		return rpc.Errorf(rpc.Unavailable, "The TLS certificate of %s cannot be verified (%v). If you trust this server, allow unverified certificates in the connection.", spec.Host, cert.Err)
	case spec.TLS != "none" && (strings.Contains(err.Error(), "AUTH") || strings.Contains(err.Error(), "tls:")):
		return rpc.Errorf(rpc.Unavailable, "The TLS handshake with %s failed (%v). Check the security setting of the connection.", spec.Host, err)
	}
	var te *textproto.Error
	if errors.As(err, &te) {
		return rpc.Errorf(rpc.Unavailable, "The FTP server answered: %d %s", te.Code, te.Msg)
	}
	return rpc.Errorf(rpc.Unavailable, "Could not connect to %s: %s", spec.address(), netErrText(err))
}

// ftpErr names the usual replies. 550 means "unavailable", which servers use
// both for "missing" and for "not allowed": the text decides.
func ftpErr(op, p string, err error) error {
	var te *textproto.Error
	if !errors.As(err, &te) {
		return err
	}
	if te.Code == 421 {
		return io.ErrUnexpectedEOF
	}
	base := ftpBaseErr(te)
	if base == nil {
		return rpc.Errorf(rpc.Internal, "%s %s: the server answered %d %s", op, p, te.Code, te.Msg)
	}
	return &os.PathError{Op: op, Path: p, Err: &ftpMsgErr{base: base, msg: te.Msg}}
}

// ftpBaseErr maps a reply to the os error it stands for, or nil when unknown.
func ftpBaseErr(te *textproto.Error) error {
	switch te.Code {
	case 550:
		return ftp550Err(strings.ToLower(te.Msg))
	case 553, 532:
		return os.ErrPermission
	}
	return nil
}

func ftp550Err(msg string) error {
	switch {
	case containsAny(msg, "no such", "not found", "not exist", "cannot find"):
		return os.ErrNotExist
	case containsAny(msg, "exists", "already"):
		return os.ErrExist
	case containsAny(msg, "permission", "denied", "not allowed"):
		return os.ErrPermission
	}
	return nil
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

type ftpMsgErr struct {
	base error
	msg  string
}

func (e *ftpMsgErr) Error() string { return e.msg }
func (e *ftpMsgErr) Unwrap() error { return e.base }

func ftpEntry(dir string, it *ftp.Entry) Entry {
	e := Entry{Name: it.Name, Path: path.Join(dir, it.Name), Size: int64(it.Size), MTime: it.Time.UnixMilli()}
	switch it.Type {
	case ftp.EntryTypeFolder:
		e.Type, e.Size, e.Mode, e.Perm = "dir", 0, "0755", "rwxr-xr-x"
	case ftp.EntryTypeLink:
		e.Type, e.Target, e.Mode, e.Perm = "symlink", it.Target, "0777", "rwxrwxrwx"
	default:
		e.Type, e.Mode, e.Perm = "file", "0644", "rw-r--r--"
		e.Mime = mimeOf(it.Name)
	}
	return e
}

func (f *ftpFS) List(ctx context.Context, dir string) ([]Entry, error) {
	its, err := f.c.List(dir)
	if err != nil {
		return nil, ftpErr("list", dir, err)
	}
	out := make([]Entry, 0, len(its))
	for _, it := range its {
		if it.Name == "." || it.Name == ".." || it.Name == "" || strings.Contains(it.Name, "/") {
			continue
		}
		e := ftpEntry(dir, it)
		if e.Type == "symlink" {
			// Ask whether the link leads to a folder.
			if cerr := f.c.ChangeDir(e.Path); cerr == nil {
				e.TargetType = "dir"
			} else {
				e.TargetType = "file"
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (f *ftpFS) Stat(ctx context.Context, p string) (Entry, error) {
	if p == "/" {
		return dirEntry("/"), nil
	}
	if it, err := f.c.GetEntry(p); err == nil && it != nil {
		e := ftpEntry(path.Dir(p), it)
		e.Name, e.Path = path.Base(p), p
		return e, nil
	}
	// No MLST: find it in the parent's listing.
	its, err := f.c.List(path.Dir(p))
	if err != nil {
		return Entry{}, ftpErr("stat", p, err)
	}
	name := path.Base(p)
	for _, it := range its {
		if it.Name == name {
			e := ftpEntry(path.Dir(p), it)
			if e.Type == "symlink" {
				if f.c.ChangeDir(p) == nil {
					e.TargetType = "dir"
				} else {
					e.TargetType = "file"
				}
			}
			return e, nil
		}
	}
	return Entry{}, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
}

func (f *ftpFS) Mkdir(ctx context.Context, p string) error {
	return ftpErr("mkdir", p, f.c.MakeDir(p))
}

func (f *ftpFS) Rename(ctx context.Context, from, to string) error {
	return ftpErr("rename", from, f.c.Rename(from, to))
}

func (f *ftpFS) Remove(ctx context.Context, p string, dir bool) error {
	if dir {
		return ftpErr("remove", p, f.c.RemoveDir(p))
	}
	return ftpErr("remove", p, f.c.Delete(p))
}

func (f *ftpFS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	r, err := f.c.Retr(p)
	if err != nil {
		return nil, ftpErr("open", p, err)
	}
	return r, nil
}

func (f *ftpFS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	return newPipeWriter(func(r io.Reader) error {
		return ftpErr("write", p, f.c.Stor(p, r))
	}), nil
}

func (f *ftpFS) Close() error { return f.c.Quit() }

// alive checks the control connection before the pool hands it out again.
func (f *ftpFS) alive() bool { return f.c.NoOp() == nil }
