// Package prefs stores per-user preferences (theme, language, layout…) in
// ~/.config/ervisio/prefs.json. It runs in the user bridge, so each Linux
// account has its own file.
package prefs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Limits.
const (
	MaxValueSize = 256 << 10
	MaxFileSize  = 1 << 20
)

var keyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// Store is a JSON object persisted atomically.
type Store struct {
	mu   sync.Mutex
	path string // "" = resolve from $HOME on first use
}

// Register adds prefs.get, prefs.set and prefs.setAll.
func Register(r *rpc.Registry) {
	s := &Store{}
	r.Handle("prefs.get", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		return s.Get()
	})
	r.Handle("prefs.set", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return s.Update(map[string]json.RawMessage{p.Key: p.Value}, false)
	})
	r.Handle("prefs.setAll", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Values  map[string]json.RawMessage `json:"values"`
			Replace bool                       `json:"replace"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		if p.Values == nil {
			return nil, rpc.Errorf(rpc.Invalid, "values must be an object")
		}
		return s.Update(p.Values, p.Replace)
	})
}

// NewStore returns a store backed by path (used by tests).
func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) file() (string, error) {
	if s.path != "" {
		return s.path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", rpc.Errorf(rpc.Unavailable, "home directory unknown")
	}
	s.path = filepath.Join(home, brand.UserDataDir, brand.PrefsFile)
	return s.path, nil
}

// Get returns every preference.
func (s *Store) Get() (map[string]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() (map[string]json.RawMessage, error) {
	p, err := s.file()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(data)) == 0 {
		return m, nil
	}
	if len(data) > MaxFileSize || json.Unmarshal(data, &m) != nil || m == nil {
		// Keep the damaged file for inspection and start over.
		log.Printf("prefs: %s is unreadable, moving it aside", p)
		_ = os.Rename(p, p+".corrupt")
		return map[string]json.RawMessage{}, nil
	}
	return m, nil
}

// Update sets the given keys (a JSON null value deletes the key). With
// replace, keys not in values are removed. It returns the new preferences.
func (s *Store) Update(values map[string]json.RawMessage, replace bool) (map[string]json.RawMessage, error) {
	for k, v := range values {
		if !keyRe.MatchString(k) {
			return nil, rpc.Errorf(rpc.Invalid, "invalid preference key %q", k)
		}
		if len(v) > MaxValueSize {
			return nil, rpc.Errorf(rpc.Invalid, "value of %s is too large", k)
		}
		if len(v) > 0 && !json.Valid(v) {
			return nil, rpc.Errorf(rpc.Invalid, "value of %s is not valid JSON", k)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]json.RawMessage{}
	if !replace {
		var err error
		if m, err = s.load(); err != nil {
			return nil, err
		}
	}
	for k, v := range values {
		if len(v) == 0 || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "encode preferences: %v", err)
	}
	if len(data) > MaxFileSize {
		return nil, rpc.Errorf(rpc.Invalid, "preferences exceed %d bytes", MaxFileSize)
	}
	p, err := s.file()
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(p, append(data, '\n')); err != nil {
		return nil, err
	}
	return m, nil
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".prefs-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
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
	return os.Rename(tmp.Name(), path)
}
