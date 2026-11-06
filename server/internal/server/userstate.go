package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/modules/prefs"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

type personalPreferences struct {
	mu          sync.Mutex
	store       *prefs.Store
	initialized bool
}

// The authenticated account determines the namespace. No client path or admin
// flag can select another account's preferences.
func accountStateID(sess *Session) (string, error) {
	if sess == nil || sess.Account == nil || sess.Account.Name == "" {
		return "", rpc.Errorf(rpc.Forbidden, "No account is available")
	}
	a := sess.Account
	identity, _ := json.Marshal([]any{a.Name, a.UID, a.SID})
	if a.SID != "" {
		identity, _ = json.Marshal([]string{"sid", strings.ToUpper(a.SID)})
	}
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:]), nil
}

func privateStateDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err = createStateDirectory(path); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !stateOwnedBySelf(path, info) || (runtime.GOOS != "windows" && info.Mode().Perm()&0022 != 0) {
		return rpc.Errorf(rpc.Forbidden, "The managed account-state directory has unsafe ownership or permissions")
	}
	return nil
}

func (s *Server) accountStateDirectory(sess *Session) (string, error) {
	id, err := accountStateID(sess)
	if err != nil {
		return "", err
	}
	root := s.opts.StateDir
	if root == "" {
		if s.opts.Dev {
			root = filepath.Join(filepath.Dir(s.opts.ConfigPath), "user-state")
		} else {
			root = brand.StateDir
		}
	}
	// The configured daemon state root is administrator-controlled, not a request parameter.
	if err = os.MkdirAll(filepath.Dir(root), 0700); err != nil {
		return "", err
	}
	for _, path := range []string{root, filepath.Join(root, "users"), filepath.Join(root, "users", id)} {
		if err = privateStateDirectory(path); err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "users", id), nil
}

func (s *Server) preferencesFor(ctx context.Context, sess *Session) (*personalPreferences, error) {
	dir, err := s.accountStateDirectory(sess)
	if err != nil {
		return nil, err
	}
	value, _ := s.personalPrefs.LoadOrStore(dir, &personalPreferences{store: prefs.NewStore(filepath.Join(dir, "prefs.json"))})
	entry := value.(*personalPreferences)
	if err := entry.initialize(filepath.Join(dir, "prefs.json"), func() (json.RawMessage, error) {
		process, e := s.userBridge(ctx, sess)
		if e != nil {
			return nil, e
		}
		return process.Call(ctx, "prefs.get", json.RawMessage(`{}`))
	}); err != nil {
		return nil, err
	}
	return entry, nil
}

func (entry *personalPreferences) initialize(path string, readLegacy func() (json.RawMessage, error)) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.initialized {
		return nil
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || !stateOwnedBySelf(path, info) {
			return rpc.Errorf(rpc.Forbidden, "Unsafe account preferences file")
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		// Migrate readable existing preferences through the account's own bridge.
		// A read-only home can still supply preferences; all future writes stay here.
		raw, e := readLegacy()
		if e != nil {
			return e
		}
		var old map[string]json.RawMessage
		if json.Unmarshal(raw, &old) != nil || old == nil {
			return rpc.Errorf(rpc.Invalid, "Invalid legacy preferences")
		}
		if _, err = entry.store.Update(old, true); err != nil {
			return err
		}
	} else {
		return err
	}
	entry.initialized = true
	return nil
}

func (s *Server) personalPreferencesCall(ctx context.Context, sess *Session, method string, params json.RawMessage) (any, error) {
	entry, err := s.preferencesFor(ctx, sess)
	if err != nil {
		return nil, err
	}
	switch method {
	case "prefs.get":
		return entry.store.Get()
	case "prefs.set":
		var p struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err = json.Unmarshal(params, &p); err != nil {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid preferences request")
		}
		return entry.store.Update(map[string]json.RawMessage{p.Key: p.Value}, false)
	case "prefs.setAll":
		var p struct {
			Values  map[string]json.RawMessage `json:"values"`
			Replace bool                       `json:"replace"`
		}
		if err = json.Unmarshal(params, &p); err != nil || p.Values == nil {
			return nil, rpc.Errorf(rpc.Invalid, "values must be an object")
		}
		return entry.store.Update(p.Values, p.Replace)
	}
	return nil, rpc.Errorf(rpc.NotFound, "Unknown personal preferences method")
}

func (s *Server) handlePersonalPreferences(w http.ResponseWriter, r *http.Request, sess *Session, req rpcRequest) bool {
	if req.Method != "prefs.get" && req.Method != "prefs.set" && req.Method != "prefs.setAll" {
		return false
	}
	result, err := s.personalPreferencesCall(r.Context(), sess, req.Method, req.Params)
	if err != nil {
		error := rpc.ToError(err, true)
		if error.Code == rpc.NeedsAdmin {
			error = rpc.Errorf(rpc.Forbidden, "The account preferences could not be read. Check access to the account's Ervisio configuration directory.")
		}
		writeError(w, error)
		return true
	}
	writeJSON(w, http.StatusOK, struct {
		Result any `json:"result"`
	}{result})
	return true
}
