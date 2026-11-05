package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const dialTimeout = 15 * time.Second

func parseHostKey(line string) (ssh.PublicKey, error) {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	return k, err
}

func fingerprint(k ssh.PublicKey) string { return ssh.FingerprintSHA256(k) }

func formatHostKey(k ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

// hostKeyError is returned while connecting when the server's key is not
// pinned yet (Changed false) or differs from the pinned one (Changed true).
type hostKeyError struct {
	Key     ssh.PublicKey
	Changed bool
	Pinned  string
}

func (e *hostKeyError) Error() string {
	if e.Changed {
		return "The host key of the server changed (now " + fingerprint(e.Key) + ", pinned " + e.Pinned + "). Someone may be intercepting the connection. If the server was reinstalled, edit the connection and trust the new key."
	}
	return "The server's host key (" + fingerprint(e.Key) + ") is not trusted yet. Edit the connection to check and trust it."
}

func parsePrivateKey(pem, pass string) (ssh.Signer, error) {
	var (
		s   ssh.Signer
		err error
	)
	if pass != "" {
		s, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(pass))
	} else {
		s, err = ssh.ParsePrivateKey([]byte(pem))
	}
	if err != nil {
		var pm *ssh.PassphraseMissingError
		switch {
		case errors.As(err, &pm):
			return nil, rpc.Errorf(rpc.Invalid, "This private key is protected by a passphrase. Enter it too.")
		case pass != "" && strings.Contains(err.Error(), "decrypt"):
			return nil, rpc.Errorf(rpc.Invalid, "The passphrase does not unlock this private key.")
		}
		return nil, rpc.Errorf(rpc.Invalid, "The private key could not be read: %v", err)
	}
	return s, nil
}

type sftpFS struct {
	ssh  *ssh.Client
	c    *sftp.Client
	stop chan struct{}
}

func dialSFTP(ctx context.Context, spec *Conn) (*sftpFS, error) {
	auth, err := sftpAuthMethods(spec)
	if err != nil {
		return nil, err
	}
	var pinned ssh.PublicKey
	if spec.HostKey != "" {
		k, err := parseHostKey(spec.HostKey)
		if err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "The pinned host key is not valid. Edit the connection and trust the server again.")
		}
		pinned = k
	}
	cfg := sftpClientConfig(spec, auth, pinned)
	addr := spec.address()
	conn, err := dialTCP(ctx, addr)
	if err != nil {
		return nil, err
	}
	stopDial := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopDial()
	deadline := time.Now().Add(dialTimeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = conn.SetDeadline(deadline)
	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, handshakeError(spec, addr, err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(cc, chans, reqs)
	sc, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		_ = client.Close()
		return nil, rpc.Errorf(rpc.Unavailable, "The server does not offer SFTP: %v", err)
	}
	f := &sftpFS{ssh: client, c: sc, stop: make(chan struct{})}
	go f.keepalive()
	return f, nil
}

// sftpAuthMethods builds the SSH auth methods from the saved secrets.
func sftpAuthMethods(spec *Conn) ([]ssh.AuthMethod, error) {
	if spec.Auth == "key" {
		if spec.secret(secKey) == "" {
			return nil, rpc.Errorf(rpc.Invalid, "No private key is saved for this connection.")
		}
		signer, err := parsePrivateKey(spec.secret(secKey), spec.secret(secPassphrase))
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	pw := spec.secret(secPassword)
	return []ssh.AuthMethod{ssh.Password(pw), ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
		// Servers that ask for the password through keyboard-interactive.
		a := make([]string, len(qs))
		for i := range qs {
			a[i] = pw
		}
		return a, nil
	})}, nil
}

// sftpClientConfig builds the SSH config; a nil pinned key makes every
// host key an error carrying the key to trust.
func sftpClientConfig(spec *Conn, auth []ssh.AuthMethod, pinned ssh.PublicKey) *ssh.ClientConfig {
	cfg := &ssh.ClientConfig{
		User: spec.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if pinned == nil {
				return &hostKeyError{Key: k}
			}
			if !bytes.Equal(k.Marshal(), pinned.Marshal()) {
				return &hostKeyError{Key: k, Changed: true, Pinned: fingerprint(pinned)}
			}
			return nil
		},
		Timeout: dialTimeout,
	}
	if pinned != nil {
		cfg.HostKeyAlgorithms = []string{pinned.Type()}
		if pinned.Type() == ssh.KeyAlgoRSA {
			cfg.HostKeyAlgorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
		}
	}
	return cfg
}

// handshakeError turns an SSH handshake failure into the user-facing error.
func handshakeError(spec *Conn, addr string, err error) error {
	var hk *hostKeyError
	if errors.As(err, &hk) {
		return hk
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return rpc.Errorf(rpc.Forbidden, "The server refused the sign-in of %s. Check the user name and the %s.", spec.User, map[bool]string{true: "private key", false: "password"}[spec.Auth == "key"])
	}
	return rpc.Errorf(rpc.Unavailable, "The SSH handshake with %s failed: %v", addr, err)
}

// keepalive notices a dead connection (and keeps NAT mappings open).
func (f *sftpFS) keepalive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			done := make(chan error, 1)
			go func() {
				_, _, err := f.ssh.SendRequest("keepalive@openssh.com", true, nil)
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					_ = f.ssh.Close()
					return
				}
			case <-time.After(20 * time.Second):
				_ = f.ssh.Close()
				return
			case <-f.stop:
				return
			}
		}
	}
}

func (f *sftpFS) Close() error {
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
	_ = f.c.Close()
	return f.ssh.Close()
}

// home is the folder the server starts in.
func (f *sftpFS) home() string {
	if p, err := f.c.Getwd(); err == nil && strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	if p, err := f.c.RealPath("."); err == nil && strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	return "/"
}

func (f *sftpFS) entry(dir string, fi os.FileInfo) Entry {
	e := infoEntry(dir, fi)
	if st, ok := fi.Sys().(*sftp.FileStat); ok {
		setIDs(&e, st.UID, st.GID)
	}
	if e.Type == "symlink" {
		full := path.Join(dir, fi.Name())
		if t, err := f.c.ReadLink(full); err == nil {
			e.Target = t
		}
		if ti, err := f.c.Stat(full); err == nil {
			e.TargetType = infoEntry(dir, ti).Type
			if e.TargetType == "file" {
				e.Mime = infoEntry(dir, ti).Mime
			}
		}
	}
	return e
}

func (f *sftpFS) List(ctx context.Context, dir string) ([]Entry, error) {
	fis, err := f.c.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(fis))
	for _, fi := range fis {
		if fi.Name() == "." || fi.Name() == ".." {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, f.entry(dir, fi))
	}
	return out, nil
}

func (f *sftpFS) Stat(ctx context.Context, p string) (Entry, error) {
	fi, err := f.c.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	e := f.entry(path.Dir(p), fi)
	e.Path = p
	if p == "/" {
		e.Name = "/"
	}
	return e, nil
}

func (f *sftpFS) Mkdir(ctx context.Context, p string) error {
	if _, err := f.c.Lstat(p); err == nil {
		return os.ErrExist
	}
	return f.c.Mkdir(p)
}

func (f *sftpFS) Rename(ctx context.Context, from, to string) error {
	return f.c.Rename(from, to)
}

func (f *sftpFS) Remove(ctx context.Context, p string, dir bool) error {
	if dir {
		return f.c.RemoveDirectory(p)
	}
	return f.c.Remove(p)
}

func (f *sftpFS) Open(ctx context.Context, p string) (io.ReadCloser, error) {
	return f.c.Open(p)
}

func (f *sftpFS) Create(ctx context.Context, p string, size int64) (io.WriteCloser, error) {
	return f.c.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
}

func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	cctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext(cctx, "tcp", addr)
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Could not connect to %s: %s", addr, netErrText(err))
	}
	return conn, nil
}

func netErrText(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return "the server name is unknown"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "i/o timeout"):
		return "no answer (timed out)"
	case strings.Contains(err.Error(), "refused"):
		return "the connection was refused (nothing listens on that port)"
	case strings.Contains(err.Error(), "unreachable"):
		return "the network is unreachable"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}
