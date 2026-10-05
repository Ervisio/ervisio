package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/ervisio/ervisio/server/internal/account"
	"github.com/ervisio/ervisio/server/internal/bridge"
	"github.com/ervisio/ervisio/server/internal/pam"
	"github.com/ervisio/ervisio/server/internal/rpc"
	"golang.org/x/crypto/ssh"
)

// maxSessionsPerUser bounds sessions (and bridge processes) per account.
const maxSessionsPerUser = 32

// Session is a signed-in browser session.
type Session struct {
	key      string // hex(sha256(token))
	Account  *account.Account
	Created  time.Time
	Remember bool
	// RHost is the client address that signed in.
	RHost string
	// Method is how the session signed in: "password" or "ssh-key".
	Method string
	// key is the SSH key used to sign in (Method "ssh-key"); revalidation
	// checks it is still authorized.
	sshKey *sessionKey

	// expires is the absolute end of the session (see sessionLifetime).
	expires time.Time
	// shadowFP is the account's shadow fingerprint at sign-in ("" when
	// unreadable, e.g. in --dev); checked is the last revalidation.
	shadowFP string
	checked  time.Time

	spawnMu   sync.Mutex // serialises user bridge restarts
	mu        sync.Mutex
	lastSeen  time.Time
	user      *bridge.Proc
	root      *bridge.Proc
	rootUsed  time.Time
	unlocking bool
	closed    bool
	// WebSocket accounting (see ws.go limits).
	wsConns    int
	wsChannels int
	inputBytes int64
	// bigOnce/big: the slots for large request bodies (see acquireBig).
	bigOnce sync.Once
	big     chan struct{}
	done    chan struct{}
}

// Large request bodies: a /api/rpc body or a WebSocket "open" frame larger
// than bigBody is held in memory whole (up to 12 MiB) until the call ends, so
// a session may have at most maxBigBodies of them at once; more wait for a
// slot, at most bigBodyWait (security review M3).
const (
	bigBody      = 1 << 20
	maxBigBodies = 2
	bigBodyWait  = 30 * time.Second
)

// acquireBig takes one of the session's large-body slots, waiting while
// both are in use; release gives it back.
func (s *Session) acquireBig(ctx context.Context) (release func(), e *rpc.Error) {
	s.bigOnce.Do(func() { s.big = make(chan struct{}, maxBigBodies) })
	t := time.NewTimer(bigBodyWait)
	defer t.Stop()
	select {
	case s.big <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s.big }) }, nil
	case <-ctx.Done():
		return nil, rpc.Errorf(rpc.Unavailable, "The request was cancelled.")
	case <-t.C:
		return nil, rpc.Errorf(rpc.Unavailable, "Too many large requests are running for this session. Try again when they are done.")
	}
}

// Done is closed when the session ends (logout, expiry, shutdown).
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) touch(now time.Time) {
	s.mu.Lock()
	s.lastSeen = now
	s.mu.Unlock()
}

// unlockedUntil returns when admin rights expire (zero when locked).
func (s *Session) unlockedUntil(idle time.Duration) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == nil || !s.root.Alive() {
		return time.Time{}
	}
	if idle == 0 {
		// No idle limit: unlocked until the session ends.
		if !s.expires.IsZero() {
			return s.expires
		}
		return time.Now().Add(24 * time.Hour)
	}
	if s.root.Busy() {
		// Kept alive while something runs; report a full window from now.
		return time.Now().Add(idle)
	}
	return s.rootUsed.Add(idle)
}

// close stops the bridges. It is idempotent.
func (s *Session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	user, root := s.user, s.root
	s.user, s.root = nil, nil
	close(s.done)
	s.mu.Unlock()
	if root != nil {
		root.Stop()
	}
	if user != nil {
		user.Stop()
	}
}

// lock stops the root bridge.
func (s *Session) lock() {
	s.mu.Lock()
	root := s.root
	s.root = nil
	s.mu.Unlock()
	if root != nil {
		root.Stop()
	}
}

// sessionKey is the SSH key a session signed in with.
type sessionKey struct {
	pub         ssh.PublicKey
	fingerprint string
	// fromIP is the client address authorized_keys from= options were
	// matched against at sign-in ("" when unknown); revalidation uses it.
	fromIP string
}

// store holds sessions in memory, keyed by the SHA-256 of the token so the
// token itself is never kept and lookups do not leak it through timing.
type store struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func newStore() *store { return &store{sessions: map[string]*Session{}} }

func newToken() (string, error) {
	b := make([]byte, 32) // 256 bits
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// add registers a session and returns its token. The oldest sessions of the
// same user beyond maxSessionsPerUser are closed.
func (st *store) add(s *Session) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.key = tokenKey(token)
	s.done = make(chan struct{})
	var evict []*Session
	st.mu.Lock()
	st.sessions[s.key] = s
	var mine []*Session
	for _, o := range st.sessions {
		if o.Account.UID == s.Account.UID {
			mine = append(mine, o)
		}
	}
	for len(mine) > maxSessionsPerUser {
		oldest := 0
		for i, o := range mine {
			if o.Created.Before(mine[oldest].Created) {
				oldest = i
			}
		}
		evict = append(evict, mine[oldest])
		delete(st.sessions, mine[oldest].key)
		mine = append(mine[:oldest], mine[oldest+1:]...)
	}
	st.mu.Unlock()
	for _, o := range evict {
		go func() { o.close(); st.forgetIfIdle(o.Account.Name) }()
	}
	return token, nil
}

// get returns the session for token, or nil.
func (st *store) get(token string) *Session {
	if token == "" || len(token) > 128 {
		return nil
	}
	key := tokenKey(token)
	st.mu.Lock()
	s := st.sessions[key]
	st.mu.Unlock()
	if s == nil || subtle.ConstantTimeCompare([]byte(s.key), []byte(key)) != 1 {
		return nil
	}
	return s
}

func (st *store) remove(s *Session) {
	st.mu.Lock()
	if st.sessions[s.key] == s {
		delete(st.sessions, s.key)
	}
	st.mu.Unlock()
	s.close()
	st.forgetIfIdle(s.Account.Name)
}

// forgetIfIdle drops the logon credential pam kept for name (Windows token;
// no-op elsewhere) when no live session of that user remains.
func (st *store) forgetIfIdle(name string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, o := range st.sessions {
		if strings.EqualFold(o.Account.Name, name) {
			return
		}
	}
	pam.Forget(name)
}

// expire closes sessions idle for longer than timeout or past their
// absolute lifetime, and locks root bridges idle for longer than adminIdle.
func (st *store) expire(now time.Time, timeout, adminIdle time.Duration) {
	var dead, lockable []*Session
	st.mu.Lock()
	for k, s := range st.sessions {
		s.mu.Lock()
		idle := now.Sub(s.lastSeen)
		over := !s.expires.IsZero() && now.After(s.expires)
		rootIdle := s.root != nil && ((adminIdle > 0 && !s.root.Busy() && now.Sub(s.rootUsed) > adminIdle) || !s.root.Alive())
		s.mu.Unlock()
		if idle > timeout || over {
			delete(st.sessions, k)
			dead = append(dead, s)
		} else if rootIdle {
			lockable = append(lockable, s)
		}
	}
	st.mu.Unlock()
	for _, s := range dead {
		go func() { s.close(); st.forgetIfIdle(s.Account.Name) }()
	}
	for _, s := range lockable {
		go s.lock()
	}
}

func (st *store) all() []*Session {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]*Session, 0, len(st.sessions))
	for _, s := range st.sessions {
		out = append(out, s)
	}
	return out
}
