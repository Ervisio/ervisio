// Package sshauth implements sign-in with an SSH key: one-time challenges,
// signature checks with golang.org/x/crypto/ssh and authorized_keys lookups
// that follow sshd's rules (StrictModes, from=, expiry-time=, command=).
//
// The private key never reaches the server: the browser signs
// Message(host, user, nonce) and sends the signature with the public key.
package sshauth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Prefix starts every signed message (domain separation: a signature made
// for LinuxAdmin can never be a valid SSH user-auth or SSHSIG signature,
// whose first bytes are a length or "SSHSIG").
const Prefix = "linuxadmin-ssh-auth-v1"

// ChallengeTTL is how long a challenge may be answered.
const ChallengeTTL = 60 * time.Second

// Message is the exact byte string the client signs.
func Message(host, user, nonce string) []byte {
	return []byte(Prefix + "\n" + host + "\n" + user + "\n" + nonce)
}

// Challenge is an outstanding nonce.
type Challenge struct {
	Nonce   string
	User    string
	IP      string // client address the challenge was issued to
	Key     string // rate-limit key of IP (IPv4 or IPv6 /64), for quotas
	Host    string // host name the browser used (signed)
	Expires time.Time
}

// Errors from Issue and Take.
var (
	// ErrFull means too many challenges are outstanding.
	ErrFull = errors.New("too many sign-in challenges outstanding")
	// ErrChallenge means the nonce is unknown, used, expired, or was issued
	// for another user or client address.
	ErrChallenge = errors.New("invalid or expired challenge")
)

// Store keeps outstanding challenges in memory. Each is single use and
// bound to user, client address and host.
type Store struct {
	mu      sync.Mutex
	m       map[string]*Challenge
	order   []string // nonces in issue order (oldest first)
	Max     int      // total outstanding challenges
	PerKey  int      // outstanding challenges per client key
	TTL     time.Duration
	Now     func() time.Time
	randSrc func([]byte) (int, error)
}

// NewStore returns a store with the default bounds (4096 total, 8 per
// client key, 60 s).
func NewStore() *Store {
	return &Store{m: map[string]*Challenge{}, Max: 4096, PerKey: 8, TTL: ChallengeTTL, Now: time.Now, randSrc: rand.Read}
}

// Issue creates a challenge. When the client key already holds PerKey
// challenges its oldest one is dropped; when the store is full of live
// challenges it returns ErrFull.
func (s *Store) Issue(user, ip, key, host string) (*Challenge, error) {
	b := make([]byte, 32)
	if _, err := s.randSrc(b); err != nil {
		return nil, err
	}
	now := s.Now()
	c := &Challenge{Nonce: base64.RawURLEncoding.EncodeToString(b), User: user, IP: ip, Key: key, Host: host, Expires: now.Add(s.TTL)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	var mine []string
	for _, n := range s.order {
		if s.m[n].Key == key {
			mine = append(mine, n)
		}
	}
	for len(mine) >= s.PerKey && len(mine) > 0 {
		s.deleteLocked(mine[0])
		mine = mine[1:]
	}
	if len(s.m) >= s.Max {
		return nil, ErrFull
	}
	s.m[c.Nonce] = c
	s.order = append(s.order, c.Nonce)
	cp := *c
	return &cp, nil
}

// Take consumes the nonce: it is removed whatever the outcome, so a nonce
// can be tried once. It succeeds only for the same user and client address
// before expiry.
func (s *Store) Take(nonce, user, ip string) (*Challenge, error) {
	if len(nonce) != 43 { // base64url of 32 bytes
		return nil, ErrChallenge
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.m[nonce]
	if c == nil {
		return nil, ErrChallenge
	}
	s.deleteLocked(nonce)
	if !s.Now().Before(c.Expires) || c.User != user || c.IP != ip {
		return nil, ErrChallenge
	}
	return c, nil
}

// Len returns the number of stored challenges (live or not yet pruned).
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}

// Prune drops expired challenges.
func (s *Store) Prune() {
	s.mu.Lock()
	s.pruneLocked(s.Now())
	s.mu.Unlock()
}

func (s *Store) pruneLocked(now time.Time) {
	i := 0
	for i < len(s.order) {
		c := s.m[s.order[i]]
		if c != nil && now.Before(c.Expires) {
			break
		}
		delete(s.m, s.order[i])
		i++
	}
	s.order = s.order[i:]
}

func (s *Store) deleteLocked(nonce string) {
	delete(s.m, nonce)
	for i, n := range s.order {
		if n == nonce {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}
