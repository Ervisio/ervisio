package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

// MaxSessions bounds the persistent sessions held by one bridge.
const MaxSessions = 16

// Session kinds.
const (
	KindLocal = "local"
	KindRoot  = "root"
	KindSSH   = "ssh"
)

// spec describes the process of a new session.
type spec struct {
	Name  string
	Kind  string
	Path  string   // executable
	Argv  []string // full argv including argv[0]
	Dir   string
	Env   []string
	Cols  uint16
	Rows  uint16
	Shell string // for the default title
}

// Info is what terminal.list returns for one session.
type Info struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	CreatedAt int64  `json:"createdAt"`
	Attached  bool   `json:"attached"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
}

// subscriber receives live output of a session.
type subscriber struct {
	ch      chan []byte
	dropped chan struct{} // closed when the subscriber fell too far behind
	once    sync.Once
}

func (s *subscriber) drop() { s.once.Do(func() { close(s.dropped) }) }

// Session is one persistent pty with its process.
type Session struct {
	id       string
	kind     string
	created  time.Time
	shell    string
	startDir string

	cmd  *exec.Cmd
	ptmx *os.File
	ring *ring

	mu    sync.Mutex
	name  string
	title string
	subs  map[*subscriber]struct{}
	osc   oscState

	done     chan struct{} // closed after the process ended and output was drained
	exitCode int
	onExit   func(*Session)
}

// Manager owns the sessions of a bridge process.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func newManager() *Manager { return &Manager{sessions: map[string]*Session{}} }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create starts the process in a new pty and registers the session.
func (m *Manager) Create(sp spec) (*Session, error) {
	m.mu.Lock()
	n := len(m.sessions)
	m.mu.Unlock()
	if n >= MaxSessions {
		return nil, rpc.Errorf(rpc.Conflict, "There are already %d sessions open. Close one before starting another.", MaxSessions)
	}
	cmd := &exec.Cmd{Path: sp.Path, Args: sp.Argv, Dir: sp.Dir, Env: sp.Env}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: sp.Cols, Rows: sp.Rows})
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Could not start %s: %v", sp.Path, err)
	}
	s := &Session{
		id: newID(), kind: sp.Kind, created: time.Now(), shell: sp.Shell, startDir: sp.Dir,
		cmd: cmd, ptmx: ptmx, ring: newRing(ScrollbackSize),
		name: sp.Name, subs: map[*subscriber]struct{}{}, done: make(chan struct{}),
	}
	s.onExit = func(s *Session) {
		m.mu.Lock()
		delete(m.sessions, s.id)
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
	go s.pump()
	return s, nil
}

// uniqueName returns base, or "base 2", "base 3"... when the name is taken.
func (m *Manager) uniqueName(base string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	taken := map[string]bool{}
	for _, s := range m.sessions {
		s.mu.Lock()
		taken[s.name] = true
		s.mu.Unlock()
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s %d", base, i)
	}
	return name
}

// Get returns a session or a not_found error.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s == nil {
		return nil, rpc.Errorf(rpc.NotFound, "That session no longer exists. It may have ended when its shell exited.")
	}
	return s, nil
}

// List returns every session, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	sort.Slice(all, func(i, j int) bool { return all[i].created.Before(all[j].created) })
	out := make([]Info, 0, len(all))
	for _, s := range all {
		out = append(out, s.Info())
	}
	return out
}

// CloseAll kills every session (used by tests).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.Kill(time.Second)
	}
}

// pump reads the pty until the process ends, feeding the ring and subscribers.
func (s *Session) pump() {
	buf := make([]byte, 32<<10)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.ring.Write(chunk)
			if t, ok := s.osc.feed(chunk); ok {
				s.title = t
			}
			for sub := range s.subs {
				select {
				case sub.ch <- chunk:
				default:
					sub.drop()
					delete(s.subs, sub)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.EIO) && !errors.Is(err, os.ErrClosed) {
				// unexpected read error: treat like the end of the session
				_ = err
			}
			break
		}
	}
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	_ = s.ptmx.Close()
	s.exitCode = code
	if s.onExit != nil {
		s.onExit(s)
	}
	close(s.done)
}

// Subscribe returns the scrollback and a live output channel, atomically.
func (s *Session) Subscribe() (replay []byte, sub *subscriber) {
	sub = &subscriber{ch: make(chan []byte, 512), dropped: make(chan struct{})}
	s.mu.Lock()
	defer s.mu.Unlock()
	replay = s.ring.Snapshot()
	s.subs[sub] = struct{}{}
	return replay, sub
}

// Unsubscribe detaches sub.
func (s *Session) Unsubscribe(sub *subscriber) {
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
}

// Write sends keyboard input to the process.
func (s *Session) Write(p []byte) error {
	select {
	case <-s.done:
		return nil
	default:
	}
	_, err := s.ptmx.Write(p)
	return err
}

// Resize changes the pty size.
func (s *Session) Resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Rename changes the display name.
func (s *Session) Rename(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

// Done is closed when the session has ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// ExitCode is valid after Done is closed.
func (s *Session) ExitCode() int { return s.exitCode }

// Kill hangs up the process group, then kills it after grace.
func (s *Session) Kill(grace time.Duration) {
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGHUP)
	_ = syscall.Kill(pid, syscall.SIGHUP)
	select {
	case <-s.done:
		return
	case <-time.After(grace):
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}

// Info describes the session for terminal.list.
func (s *Session) Info() Info {
	s.mu.Lock()
	name, title, attached := s.name, s.title, len(s.subs) > 0
	s.mu.Unlock()
	cwd := s.startDir
	if s.kind != KindSSH {
		if p, err := os.Readlink("/proc/" + strconv.Itoa(s.cmd.Process.Pid) + "/cwd"); err == nil {
			cwd = p
		}
	} else {
		cwd = ""
	}
	if title == "" {
		title = defaultTitle(s.kind, s.shell, cwd)
	}
	return Info{ID: s.id, Name: name, Cwd: cwd, CreatedAt: s.created.UnixMilli(), Attached: attached, Kind: s.kind, Title: title}
}

func defaultTitle(kind, shell, cwd string) string {
	if kind == KindSSH {
		return "ssh"
	}
	if cwd == "" {
		return shell
	}
	return fmt.Sprintf("%s, %s", shell, cwd)
}

// oscState extracts window titles (OSC 0 / OSC 2) from the output stream.
type oscState struct {
	in  bool
	buf []byte
}

// feed scans chunk and returns the last complete title found, if any.
func (o *oscState) feed(chunk []byte) (string, bool) {
	var title string
	found := false
	for i := 0; i < len(chunk); i++ {
		c := chunk[i]
		if !o.in {
			if c == 0x1b && i+3 < len(chunk) && chunk[i+1] == ']' && (chunk[i+2] == '0' || chunk[i+2] == '2') && chunk[i+3] == ';' {
				o.in = true
				o.buf = o.buf[:0]
				i += 3
			}
			continue
		}
		if c == 0x07 || c == 0x1b || len(o.buf) > 256 {
			o.in = false
			if c == 0x07 || c == 0x1b {
				title, found = sanitizeTitle(string(o.buf)), true
			}
			continue
		}
		o.buf = append(o.buf, c)
	}
	return title, found && title != ""
}

func sanitizeTitle(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			out = append(out, r)
		}
	}
	return string(out)
}
