package envs

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ParseSSHKey parses a PEM private key, with its passphrase when encrypted.
func ParseSSHKey(pem, passphrase string) (ssh.Signer, error) {
	var (
		s   ssh.Signer
		err error
	)
	if passphrase != "" {
		s, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pem), []byte(passphrase))
	} else {
		s, err = ssh.ParsePrivateKey([]byte(pem))
	}
	if err != nil {
		var pm *ssh.PassphraseMissingError
		switch {
		case errors.As(err, &pm):
			return nil, errf("This private key is protected by a passphrase. Enter it too.")
		case passphrase != "" && strings.Contains(err.Error(), "decrypt"):
			return nil, errf("The passphrase does not unlock this private key.")
		}
		return nil, errf("The private key could not be read: %v", err)
	}
	return s, nil
}

// ParseHostKey parses a public key in authorized_keys format.
func ParseHostKey(line string) (ssh.PublicKey, error) {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	return k, err
}

// SSHFingerprint is the usual "SHA256:<base64>" of a host key.
func SSHFingerprint(k ssh.PublicKey) string { return ssh.FingerprintSHA256(k) }

// FormatHostKey writes a key in authorized_keys format without the newline.
func FormatHostKey(k ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

// ProbeSSH connects, learns the server's host key and hangs up. The
// fingerprint and key are shown to the admin, who confirms them (the key is
// then pinned in the environment).
func ProbeSSH(ctx context.Context, addr, user string) (fingerprint, hostKey string, err error) {
	if _, _, err := SplitHostPort(addr); err != nil {
		return "", "", err
	}
	var seen ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: user,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			seen = k
			return errProbed
		},
		Timeout: dialTimeout,
	}
	cctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, derr := (&net.Dialer{}).DialContext(cctx, "tcp", addr)
	if derr != nil {
		return "", "", errf("Could not connect to %s: %v", addr, cleanNetErr(derr))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	_, _, _, herr := ssh.NewClientConn(conn, addr, cfg)
	if seen == nil {
		return "", "", errf("The server did not answer the SSH handshake: %v", herr)
	}
	return SSHFingerprint(seen), FormatHostKey(seen), nil
}

var errProbed = errors.New("host key learned")

// sshPool keeps one SSH connection per environment and opens a
// direct-streamlocal channel to the remote Docker socket for every
// connection of a tunnel. The connection is dialled on demand, kept alive
// with keepalive requests and replaced when it breaks.
type sshPool struct {
	env *Env

	mu     sync.Mutex
	client *ssh.Client
	dialMu sync.Mutex
}

func newSSHPool(e *Env) *sshPool { return &sshPool{env: e} }

func (p *sshPool) config() (*ssh.ClientConfig, error) {
	signer, err := ParseSSHKey(p.env.Secret(SecretSSHKey), p.env.Secret(SecretPassphrase))
	if err != nil {
		return nil, err
	}
	pinned, err := ParseHostKey(p.env.HostKey)
	if err != nil {
		return nil, errf("The pinned host key is not valid; edit the environment and confirm it again.")
	}
	return &ssh.ClientConfig{
		User: p.env.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			if !bytes.Equal(k.Marshal(), pinned.Marshal()) {
				return errf("The host key of %s changed (now %s, pinned %s). Someone may be intercepting the connection; if the server was reinstalled, edit the environment and confirm the new key.",
					p.env.Address, SSHFingerprint(k), SSHFingerprint(pinned))
			}
			return nil
		},
		Timeout: dialTimeout,
	}, nil
}

func (p *sshPool) get(ctx context.Context) (*ssh.Client, error) {
	p.mu.Lock()
	c := p.client
	p.mu.Unlock()
	if c != nil {
		return c, nil
	}
	p.dialMu.Lock()
	defer p.dialMu.Unlock()
	p.mu.Lock()
	c = p.client
	p.mu.Unlock()
	if c != nil {
		return c, nil
	}
	cfg, err := p.config()
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	conn, err := (&net.Dialer{KeepAlive: 30 * time.Second}).DialContext(cctx, "tcp", p.env.Address)
	if err != nil {
		return nil, errf("Could not connect to %s: %v", p.env.Address, cleanNetErr(err))
	}
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	sc, chans, reqs, err := ssh.NewClientConn(conn, p.env.Address, cfg)
	if err != nil {
		conn.Close()
		var e *Error
		if errors.As(err, &e) {
			return nil, e
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, errf("The server refused the key for user %q. Check the user name and that the public key is in its authorized_keys.", p.env.User)
		}
		return nil, errf("SSH to %s failed: %v", p.env.Address, err)
	}
	_ = conn.SetDeadline(time.Time{})
	c = ssh.NewClient(sc, chans, reqs)
	p.mu.Lock()
	p.client = c
	p.mu.Unlock()
	go p.keepAlive(c)
	return c, nil
}

func (p *sshPool) keepAlive(c *ssh.Client) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
	for {
		select {
		case <-done:
			p.drop(c)
			return
		case <-t.C:
			if _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				p.drop(c)
				c.Close()
				return
			}
		}
	}
}

func (p *sshPool) drop(c *ssh.Client) {
	p.mu.Lock()
	if p.client == c {
		p.client = nil
	}
	p.mu.Unlock()
}

// Close hangs up the connection.
func (p *sshPool) Close() {
	p.mu.Lock()
	c := p.client
	p.client = nil
	p.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

type streamLocalPayload struct {
	SocketPath string
	Reserved0  string
	Reserved1  uint32
}

// Dial opens a channel to the remote Docker socket.
func (p *sshPool) Dial(ctx context.Context) (net.Conn, error) {
	for attempt := 0; ; attempt++ {
		c, err := p.get(ctx)
		if err != nil {
			return nil, err
		}
		ch, reqs, err := c.OpenChannel("direct-streamlocal@openssh.com", ssh.Marshal(&streamLocalPayload{SocketPath: p.env.SocketPath}))
		if err == nil {
			go ssh.DiscardRequests(reqs)
			return &chanConn{Channel: ch, local: c.LocalAddr(), remote: c.RemoteAddr()}, nil
		}
		var oe *ssh.OpenChannelError
		if errors.As(err, &oe) {
			// The server answered: forwarding is off, or the socket is not
			// there or not readable. A new connection would not help.
			msg := oe.Message
			switch oe.Reason {
			case ssh.Prohibited:
				return nil, errf("The SSH server forbids forwarding to a socket. In its sshd_config set AllowStreamLocalForwarding yes (or local) and AllowTcpForwarding yes (or local), then reload sshd; also check the user's key in authorized_keys is not restricted with no-port-forwarding.")
			case ssh.ConnectionFailed:
				// sshd answers "open failed" both for a socket it cannot open and for
				// forwarding it forbids, so the message names both causes.
				return nil, errf("The SSH server could not open %s (%s). Check that Docker runs there and that user %q may use the socket (the docker group), and that sshd allows socket forwarding: AllowStreamLocalForwarding yes (or local) and AllowTcpForwarding yes (or local) in sshd_config.", p.env.SocketPath, msg, p.env.User)
			}
			return nil, errf("The SSH server refused the channel: %s", msg)
		}
		// The connection is gone: drop it and retry once with a fresh one.
		p.drop(c)
		c.Close()
		if attempt >= 1 {
			return nil, errf("The SSH connection to %s broke: %v", p.env.Address, err)
		}
	}
}

// chanConn adapts an ssh.Channel to net.Conn.
type chanConn struct {
	ssh.Channel
	local, remote net.Addr
}

func (c *chanConn) LocalAddr() net.Addr              { return c.local }
func (c *chanConn) RemoteAddr() net.Addr             { return c.remote }
func (c *chanConn) SetDeadline(time.Time) error      { return nil }
func (c *chanConn) SetReadDeadline(time.Time) error  { return nil }
func (c *chanConn) SetWriteDeadline(time.Time) error { return nil }

// CloseWrite sends EOF on the channel so the peer sees a half-close.
func (c *chanConn) CloseWrite() error { return c.Channel.CloseWrite() }
