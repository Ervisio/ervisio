package envs

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Pairing is the server side (B) of a pairing: another Ervisio server holds
// a credential whose hash is stored here.
type Pairing struct {
	ID string `json:"id"`
	// Name is what the other server calls itself.
	Name string `json:"name"`
	// CredHash is hex(sha256(credential)); the credential itself is never kept.
	CredHash string `json:"credHash"`
	// User is the local Linux user every call of this pairing runs as.
	User      string    `json:"user"`
	CreatedBy string    `json:"createdBy"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"lastUsed,omitempty"`
	LastVia   string    `json:"lastVia,omitempty"`
	LastAddr  string    `json:"lastAddr,omitempty"`
}

// record is an environment in the file: its public fields and its sealed secrets.
type record struct {
	*Env
	Sealed map[string]string `json:"sealed,omitempty"`
}

type fileData struct {
	Version  int       `json:"version"`
	Envs     []record  `json:"envs"`
	Pairings []Pairing `json:"pairings"`
	// AgentKey is the sealed ECDSA key used to sign Portainer agent requests.
	AgentKey string `json:"agentKey,omitempty"`
}

// store keeps environments in <dir>/envs.json (mode 0600, written
// atomically). Secrets are sealed with <dir>/master.key (0600): a copy of
// envs.json alone (a backup, a screenshot of the file) does not reveal them.
type store struct {
	dir string

	mu   sync.Mutex
	gcm  cipher.AEAD
	data fileData
}

func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	s := &store{dir: dir}
	if err := s.loadKey(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "envs.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.data = fileData{Version: 1}
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("envs.json: %w", err)
		}
	}
	for i := range s.data.Envs {
		r := &s.data.Envs[i]
		if r.Env == nil {
			return nil, errors.New("envs.json: empty environment")
		}
		r.secrets = map[string]string{}
		for k, v := range r.Sealed {
			p, err := s.open(v)
			if err != nil {
				return nil, fmt.Errorf("envs.json: %s: cannot unseal %s (was master.key replaced?)", r.ID, k)
			}
			r.secrets[k] = p
		}
		r.Sealed = nil
	}
	return s, nil
}

func (s *store) loadKey() error {
	p := filepath.Join(s.dir, "master.key")
	key, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, werr := f.Write(key)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
	} else if err != nil {
		return err
	}
	if len(key) != 32 {
		return errors.New("master.key is not 32 bytes")
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	s.gcm, err = cipher.NewGCM(blk)
	return err
}

func (s *store) seal(plain string) string {
	nonce := make([]byte, s.gcm.NonceSize())
	_, _ = rand.Read(nonce)
	return "v1:" + base64.StdEncoding.EncodeToString(s.gcm.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *store) open(sealed string) (string, error) {
	if len(sealed) < 4 || sealed[:3] != "v1:" {
		return "", errors.New("unknown format")
	}
	raw, err := base64.StdEncoding.DecodeString(sealed[3:])
	if err != nil || len(raw) < s.gcm.NonceSize() {
		return "", errors.New("bad data")
	}
	n := s.gcm.NonceSize()
	p, err := s.gcm.Open(nil, raw[:n], raw[n:], nil)
	return string(p), err
}

// save writes the file; the caller holds s.mu.
func (s *store) save() error {
	out := fileData{Version: 1, Pairings: s.data.Pairings, AgentKey: s.data.AgentKey}
	for _, r := range s.data.Envs {
		rec := record{Env: r.Env, Sealed: map[string]string{}}
		for k, v := range r.secrets {
			if v != "" {
				rec.Sealed[k] = s.seal(v)
			}
		}
		out.Envs = append(out.Envs, rec)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".envs-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
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
	return os.Rename(tmp.Name(), filepath.Join(s.dir, "envs.json"))
}

func (s *store) list() []*Env {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Env, 0, len(s.data.Envs))
	for _, r := range s.data.Envs {
		out = append(out, r.Env.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (s *store) get(id string) *Env {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.data.Envs {
		if r.ID == id {
			return r.Env.clone()
		}
	}
	return nil
}

func newID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "env-" + hex.EncodeToString(b[:])
}

// put adds or replaces an environment.
func (s *store) put(e *Env) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.Envs
	replaced := false
	for i := range s.data.Envs {
		if s.data.Envs[i].ID == e.ID {
			s.data.Envs[i] = record{Env: e.clone()}
			replaced = true
		}
	}
	if !replaced {
		if len(s.data.Envs) >= MaxEnvs {
			return errf("There are already %d environments, the most this server keeps.", MaxEnvs)
		}
		s.data.Envs = append(s.data.Envs, record{Env: e.clone()})
	}
	if err := s.save(); err != nil {
		s.data.Envs = old
		return err
	}
	return nil
}

func (s *store) remove(id string) (*Env, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.Envs
	var out []record
	var gone *Env
	for _, r := range s.data.Envs {
		if r.ID == id {
			gone = r.Env
			continue
		}
		out = append(out, r)
	}
	if gone == nil {
		return nil, nil
	}
	s.data.Envs = out
	if err := s.save(); err != nil {
		s.data.Envs = old
		return nil, err
	}
	return gone, nil
}

// agentKey returns the sealed agent signing key PEM ("" when none yet).
func (s *store) agentKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.AgentKey == "" {
		return ""
	}
	p, err := s.open(s.data.AgentKey)
	if err != nil {
		return ""
	}
	return p
}

func (s *store) setAgentKey(pem string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.AgentKey
	s.data.AgentKey = s.seal(pem)
	if err := s.save(); err != nil {
		s.data.AgentKey = old
		return err
	}
	return nil
}

func (s *store) pairings() []Pairing {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Pairing{}, s.data.Pairings...)
}

func (s *store) addPairing(p Pairing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.Pairings
	s.data.Pairings = append(append([]Pairing{}, old...), p)
	if err := s.save(); err != nil {
		s.data.Pairings = old
		return err
	}
	return nil
}

func (s *store) removePairing(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.data.Pairings
	var out []Pairing
	found := false
	for _, p := range old {
		if p.ID == id {
			found = true
			continue
		}
		out = append(out, p)
	}
	if !found {
		return false, nil
	}
	s.data.Pairings = out
	if err := s.save(); err != nil {
		s.data.Pairings = old
		return false, err
	}
	return true, nil
}

// touchPairing records a use (in memory only unless a minute passed).
func (s *store) touchPairing(id, via, addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Pairings {
		p := &s.data.Pairings[i]
		if p.ID == id {
			stale := time.Since(p.LastUsed) > time.Minute
			p.LastUsed, p.LastVia, p.LastAddr = time.Now(), via, addr
			if stale {
				_ = s.save()
			}
			return
		}
	}
}

func hashCred(c string) string {
	h := sha256.Sum256([]byte(c))
	return hex.EncodeToString(h[:])
}
