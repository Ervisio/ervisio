// Package plugindata keeps bounded, revisioned plugin documents in daemon-private
// storage. The caller supplies an authenticated account/plugin directory.
package plugindata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

const MaxDocument = 4 << 20
const MaxTotal = 64 << 20
const MaxDocuments = 500

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,159}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Document struct {
	Text     string `json:"text"`
	Revision string `json:"revision"`
}
type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}
type Store struct {
	mu   sync.Mutex
	root string
	now  func() time.Time
}

func New(root string) *Store { return &Store{root: root, now: time.Now} }

func ValidKey(key string) bool {
	if !keyPattern.MatchString(key) || strings.Contains(key, "//") || strings.HasSuffix(key, "/") {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func (s *Store) paths(key string) (string, string, error) {
	if !ValidKey(key) {
		return "", "", rpc.Errorf(rpc.Invalid, "Invalid private document key")
	}
	name := filepath.Join(s.root, digest([]byte(key)))
	return name + ".data", name + ".meta", nil
}
func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit {
		return nil, rpc.Errorf(rpc.Invalid, "Unsafe or oversized private document")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	pinned, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, pinned) {
		return nil, rpc.Errorf(rpc.Conflict, "Private document changed during read")
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, rpc.Errorf(rpc.Invalid, "Private document is too large")
	}
	return body, nil
}
func (s *Store) read(key string) (Document, error) {
	path, _, err := s.paths(key)
	if err != nil {
		return Document{}, err
	}
	body, err := readBounded(path, MaxDocument)
	if err != nil {
		return Document{}, err
	}
	return Document{Text: string(body), Revision: digest(body)}, nil
}
func (s *Store) Read(key string) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(key)
}
func (s *Store) list() ([]Entry, error) {
	files, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".meta") {
			continue
		}
		body, err := readBounded(filepath.Join(s.root, file.Name()), 1024)
		if err != nil {
			return nil, err
		}
		var meta struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(body, &meta) != nil || !ValidKey(meta.Key) {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid private document metadata")
		}
		path, metadata, err := s.paths(meta.Key)
		if err != nil {
			return nil, err
		}
		if filepath.Base(metadata) != file.Name() {
			return nil, rpc.Errorf(rpc.Invalid, "Invalid private document identity")
		}
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > MaxDocument {
			return nil, rpc.Errorf(rpc.Invalid, "Unsafe private document")
		}
		result = append(result, Entry{Path: meta.Key, Type: "file", Size: info.Size()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
func (s *Store) List() ([]Entry, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.list() }

// Write is compare-and-swap: an empty revision creates a new document; an exact
// prior revision is required to replace one. Concurrent views never silently
// replace a newer conversation with an old snapshot.
func (s *Store) Write(key, text, expected string, owners ...string) (Document, error) {
	if len(text) > MaxDocument {
		return Document{}, rpc.Errorf(rpc.Invalid, "Private document exceeds 4 MiB")
	}
	if expected != "" && !revisionPattern.MatchString(expected) {
		return Document{}, rpc.Errorf(rpc.Invalid, "Invalid document revision")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, meta, err := s.paths(key)
	if err != nil {
		return Document{}, err
	}
	owner := ""
	if len(owners) > 0 {
		owner = owners[0]
	}
	lease, err := s.lease(key)
	if err != nil {
		return Document{}, err
	}
	if lease.Expires > s.now().UnixMilli() && lease.Owner != owner {
		return Document{}, rpc.Errorf(rpc.Conflict, "This document is being updated in another view.")
	}
	current, err := s.read(key)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Document{}, err
	}
	if (exists && current.Revision != expected) || (!exists && expected != "") {
		return Document{}, rpc.Errorf(rpc.Conflict, "This document changed in another view. Reload it before saving again.")
	}
	entries, err := s.list()
	if err != nil {
		return Document{}, err
	}
	var total int64
	for _, item := range entries {
		if item.Path != key {
			total += item.Size
		}
	}
	if total+int64(len(text)) > MaxTotal || (!exists && len(entries) >= MaxDocuments) {
		return Document{}, rpc.Errorf(rpc.Invalid, "Private plugin storage is full. Export or remove older documents first.")
	}
	metadata, _ := json.Marshal(struct {
		Key string `json:"key"`
	}{key})
	if err = config.WriteAtomic(meta, metadata, 0600); err != nil {
		return Document{}, err
	}
	if err = config.WriteAtomic(path, []byte(text), 0600); err != nil {
		return Document{}, err
	}
	return Document{Text: "", Revision: digest([]byte(text))}, nil
}
func (s *Store) Remove(key, expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.read(key)
	if err != nil {
		return err
	}
	if current.Revision != expected {
		return rpc.Errorf(rpc.Conflict, "This document changed in another view. Reload it before deleting.")
	}
	lease, err := s.lease(key)
	if err != nil {
		return err
	}
	if lease.Expires > s.now().UnixMilli() {
		return rpc.Errorf(rpc.Conflict, "This document is being updated in another view.")
	}
	path, meta, _ := s.paths(key)
	if err = os.Remove(path); err != nil {
		return err
	}
	_ = os.Remove(path + ".lease")
	return os.Remove(meta)
}

type Lease struct {
	Owner   string `json:"owner"`
	Expires int64  `json:"expires"`
}

var ownerPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *Store) lease(key string) (Lease, error) {
	path, _, err := s.paths(key)
	if err != nil {
		return Lease{}, err
	}
	body, err := readBounded(path+".lease", 1024)
	if errors.Is(err, fs.ErrNotExist) {
		return Lease{}, nil
	}
	if err != nil {
		return Lease{}, err
	}
	var lease Lease
	if json.Unmarshal(body, &lease) != nil || !ownerPattern.MatchString(lease.Owner) {
		return Lease{}, rpc.Errorf(rpc.Invalid, "Invalid document lease")
	}
	return lease, nil
}

// Claim renews a short, server-timed lease. A crashed client cannot retain it.
func (s *Store) Claim(key, owner string) (Lease, error) {
	if !ownerPattern.MatchString(owner) {
		return Lease{}, rpc.Errorf(rpc.Invalid, "Invalid document owner")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _, err := s.paths(key)
	if err != nil {
		return Lease{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Lease{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Lease{}, rpc.Errorf(rpc.Invalid, "Unsafe private document")
	}
	current, err := s.lease(key)
	if err != nil {
		return Lease{}, err
	}
	if current.Expires > s.now().UnixMilli() && current.Owner != owner {
		return Lease{}, rpc.Errorf(rpc.Conflict, "This conversation is active in another chat. Wait for its reply to finish.")
	}
	next := Lease{Owner: owner, Expires: s.now().Add(2 * time.Minute).UnixMilli()}
	body, _ := json.Marshal(next)
	if err = config.WriteAtomic(path+".lease", body, 0600); err != nil {
		return Lease{}, err
	}
	return next, nil
}
func (s *Store) Release(key, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.lease(key)
	if err != nil {
		return err
	}
	if current.Owner == "" {
		return nil
	}
	if current.Owner != owner {
		return rpc.Errorf(rpc.Conflict, "The document lease belongs to another view")
	}
	path, _, err := s.paths(key)
	if err != nil {
		return err
	}
	return os.Remove(path + ".lease")
}
