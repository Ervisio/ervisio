package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Protocols.
const (
	KindSFTP   = "sftp"
	KindFTP    = "ftp"
	KindSMB    = "smb"
	KindWebDAV = "webdav"
)

// Secret names.
const (
	secPassword   = "password"
	secKey        = "key"
	secPassphrase = "passphrase"
)

// maxConns bounds the saved connections of one account.
const maxConns = 200

// Conn is a saved connection. Secrets are kept apart and never leave the
// bridge: views only say whether one is set.
type Conn struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	User string `json:"user,omitempty"`
	// Path is the folder opened first ("" = the login folder for SFTP and
	// FTP, "/" otherwise).
	Path string `json:"path,omitempty"`
	// SFTP: Auth is "password" or "key"; HostKey is the pinned server key
	// in authorized_keys format.
	Auth    string `json:"auth,omitempty"`
	HostKey string `json:"hostKey,omitempty"`
	// FTP: TLS is "explicit" (AUTH TLS, the default), "implicit" or "none".
	TLS string `json:"tls,omitempty"`
	// SMB.
	Share  string `json:"share,omitempty"`
	Domain string `json:"domain,omitempty"`
	// WebDAV: the base URL (http or https).
	URL string `json:"url,omitempty"`
	// Insecure accepts a TLS certificate that cannot be verified (FTP, WebDAV).
	Insecure bool `json:"insecure,omitempty"`
	// Updated changes on every save, so open connections notice an edit
	// made from another session.
	Updated int64 `json:"updated"`

	secrets map[string]string
}

func (c *Conn) secret(k string) string { return c.secrets[k] }

func (c *Conn) clone() *Conn {
	cp := *c
	cp.secrets = make(map[string]string, len(c.secrets))
	for k, v := range c.secrets {
		cp.secrets[k] = v
	}
	return &cp
}

// View is what the browser sees of a connection.
type View struct {
	Conn
	HasPassword   bool   `json:"hasPassword"`
	HasKey        bool   `json:"hasKey"`
	HasPassphrase bool   `json:"hasPassphrase"`
	Fingerprint   string `json:"fingerprint,omitempty"`
}

func (c *Conn) view() View {
	v := View{Conn: *c, HasPassword: c.secrets[secPassword] != "", HasKey: c.secrets[secKey] != "", HasPassphrase: c.secrets[secPassphrase] != ""}
	v.secrets = nil
	if c.HostKey != "" {
		if k, err := parseHostKey(c.HostKey); err == nil {
			v.Fingerprint = fingerprint(k)
		}
	}
	return v
}

type record struct {
	Conn
	Sealed map[string]string `json:"sealed,omitempty"`
}

type fileData struct {
	Version     int      `json:"version"`
	Connections []record `json:"connections"`
}

// store keeps the connections of the signed-in account in
// ~/.config/ervisio/remotes.json (0600). Secrets are sealed with
// remotes.key (0600) next to it, so the JSON alone (a backup, a pasted
// copy) does not reveal them. Both files belong to the account: like any
// saved password on a computer they are readable by the account and by root.
//
// The file is read on every call: every session of the account has its own
// bridge, and an edit made in one is seen by the others.
type store struct {
	mu  sync.Mutex
	dir string // "" = resolve from $HOME on first use
	gcm cipher.AEAD
}

func (s *store) base() (string, error) {
	if s.dir != "" {
		return s.dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", rpc.Errorf(rpc.Unavailable, "The home folder of this account is unknown, so connections cannot be saved.")
	}
	s.dir = filepath.Join(home, filepath.FromSlash(brand.UserDataDir))
	return s.dir, nil
}

func (s *store) cipher() (cipher.AEAD, error) {
	if s.gcm != nil {
		return s.gcm, nil
	}
	dir, err := s.base()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "remotes.key")
	key, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			// Another session made it first.
			if key, err = os.ReadFile(p); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			_, werr := f.Write(key)
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(p)
				return nil, werr
			}
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, rpc.Errorf(rpc.Internal, "%s is damaged (it must hold 32 bytes). Remove it and enter the passwords of your connections again.", p)
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	s.gcm = g
	return g, nil
}

func (s *store) seal(g cipher.AEAD, plain string) string {
	nonce := make([]byte, g.NonceSize())
	_, _ = rand.Read(nonce)
	return "v1:" + base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *store) open(g cipher.AEAD, sealed string) (string, error) {
	if len(sealed) < 4 || sealed[:3] != "v1:" {
		return "", errors.New("unknown format")
	}
	raw, err := base64.StdEncoding.DecodeString(sealed[3:])
	if err != nil || len(raw) < g.NonceSize() {
		return "", errors.New("bad data")
	}
	n := g.NonceSize()
	p, err := g.Open(nil, raw[:n], raw[n:], nil)
	return string(p), err
}

// load reads every connection; the caller holds s.mu.
func (s *store) load() ([]*Conn, error) {
	dir, err := s.base()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "remotes.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d fileData
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, rpc.Errorf(rpc.Internal, "remotes.json cannot be read: %v", err)
	}
	var g cipher.AEAD
	out := make([]*Conn, 0, len(d.Connections))
	for _, r := range d.Connections {
		c := r.Conn
		c.secrets = map[string]string{}
		for k, v := range r.Sealed {
			if g == nil {
				if g, err = s.cipher(); err != nil {
					return nil, err
				}
			}
			p, err := s.open(g, v)
			if err != nil {
				// A replaced key: keep the connection, the secret must be entered again.
				continue
			}
			c.secrets[k] = p
		}
		out = append(out, &c)
	}
	return out, nil
}

// save writes every connection atomically; the caller holds s.mu.
func (s *store) save(list []*Conn) error {
	dir, err := s.base()
	if err != nil {
		return err
	}
	g, err := s.cipher()
	if err != nil {
		return err
	}
	d := fileData{Version: 1, Connections: []record{}}
	for _, c := range list {
		r := record{Conn: *c}
		r.secrets = nil
		for k, v := range c.secrets {
			if v == "" {
				continue
			}
			if r.Sealed == nil {
				r.Sealed = map[string]string{}
			}
			r.Sealed[k] = s.seal(g, v)
		}
		d.Connections = append(d.Connections, r)
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".remotes-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "remotes.json"))
}

func (s *store) list() ([]*Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *store) get(id string) (*Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, rpc.Errorf(rpc.NotFound, "This connection does not exist any more.")
}

// put adds or replaces c (matched by ID).
func (s *store) put(c *Conn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	found := false
	for i, x := range list {
		if x.ID == c.ID {
			list[i] = c
			found = true
		}
	}
	if !found {
		if len(list) >= maxConns {
			return rpc.Errorf(rpc.Invalid, "You have %d saved connections, the most allowed. Remove one first.", maxConns)
		}
		list = append(list, c)
	}
	return s.save(list)
}

func (s *store) remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	out := list[:0]
	found := false
	for _, c := range list {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return rpc.Errorf(rpc.NotFound, "This connection does not exist any more.")
	}
	return s.save(out)
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("random: %v", err))
	}
	return hex.EncodeToString(b)
}

func validID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
