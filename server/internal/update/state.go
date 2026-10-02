package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Result is the outcome of the last update or rollback, kept in
// <StateDir>/last.json (root-owned, 0644: user bridges show it).
type Result struct {
	// State: running | ok | failed | rolled-back
	State      string `json:"state"`
	Kind       string `json:"kind"` // update | rollback
	From       string `json:"from,omitempty"`
	To         string `json:"to"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	Error      string `json:"error,omitempty"`
	// Auto: started by the daemon's automatic install.
	Auto bool `json:"auto,omitempty"`
	PID  int  `json:"pid,omitempty"`
}

// Result states.
const (
	StateRunning    = "running"
	StateOK         = "ok"
	StateFailed     = "failed"      // failed before the switch, nothing changed
	StateRolledBack = "rolled-back" // switched, the new version was unhealthy, back on the old one
)

// Kinds of switch.
const (
	KindUpdate   = "update"
	KindRollback = "rollback"
)

// State keeps the update state folder.
type State struct {
	Dir string // e.g. /var/lib/ervisio/updates (0755)
	// Owner is the uid that must own last.json for readers to trust it.
	Owner int
	// TxFile is the software module's public transaction state; while it
	// says a package transaction runs, updates do not start.
	TxFile string
	// TxOwner must own TxFile (root).
	TxOwner int
}

// DefaultState is the system state folder.
func DefaultState(dir string) *State {
	return &State{Dir: dir, Owner: 0, TxFile: brand.RunDir + "/software-transaction.public.json"}
}

func (s *State) lastPath() string { return filepath.Join(s.Dir, "last.json") }

// StagingDir is the root-only (0700) download folder.
func (s *State) StagingDir() string { return filepath.Join(s.Dir, "staging") }

// Prepare creates the state and staging folders with the right modes.
func (s *State) Prepare() error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o755); err != nil {
		return err
	}
	st := s.StagingDir()
	if fi, err := os.Lstat(st); err == nil && (!fi.IsDir() || fi.Mode()&os.ModeSymlink != 0) {
		if err := os.RemoveAll(st); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(st, 0o700); err != nil {
		return err
	}
	return os.Chmod(st, 0o700)
}

// WriteLast records r atomically.
func (s *State) WriteLast(r Result) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.lastPath() + ".tmp-" + randHex(4)
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.lastPath()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ReadLast returns the last result, or nil. A "running" result whose
// process is gone is reported as failed (interrupted).
func (s *State) ReadLast() *Result {
	b, err := readOwned(s.lastPath(), s.Owner, 64<<10)
	if err != nil {
		return nil
	}
	var r Result
	if json.Unmarshal(b, &r) != nil || r.State == "" {
		return nil
	}
	if r.State == StateRunning && !pidAlive(r.PID) {
		r.State, r.Error = StateFailed, "interrupted"
	}
	return &r
}

// Running reports whether an update or rollback is in progress.
func (s *State) Running() bool {
	r := s.ReadLast()
	return r != nil && r.State == StateRunning
}

// PackageTransactionRunning reports whether the Software section is
// installing or removing packages (restarting the daemon would kill it).
func (s *State) PackageTransactionRunning() bool {
	if s.TxFile == "" {
		return false
	}
	b, err := readOwned(s.TxFile, s.TxOwner, 1<<20)
	if err != nil {
		return false
	}
	var st struct {
		Running bool `json:"running"`
		PID     int  `json:"pid"`
	}
	if json.Unmarshal(b, &st) != nil {
		return false
	}
	return st.Running && pidAlive(st.PID)
}

// Lock takes an exclusive lock on <Dir>/lock (non-blocking). The returned
// function releases it.
func (s *State) Lock() (func(), error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "lock"), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another update is in progress")
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

// readOwned reads a regular file owned by owner (no symlinks followed).
func readOwned(path string, owner int, max int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && owner >= 0 && int(st.Uid) != owner {
		return nil, fmt.Errorf("%s has the wrong owner", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s is too large", path)
	}
	return b, nil
}

func nowMs() int64 { return time.Now().UnixMilli() }
