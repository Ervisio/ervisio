package server

import (
	"net"
	"slices"
	"sync"
	"time"
)

// failureWindow is how long failed sign-ins are remembered per client.
const failureWindow = 15 * time.Minute

// maxInflightPerKey bounds concurrent password checks (PAM or sudo) per
// client key, so one client cannot hold every global PAM slot.
const maxInflightPerKey = 2

// limiter counts failed authentications per client key (see limiterKey).
//
// An attempt is counted as a failure *before* the password is checked
// (begin) and only taken back when it succeeds (done), so concurrent
// in-flight attempts from one client cannot exceed the limit.
type limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	inflight map[string]int
	now      func() time.Time
}

func newLimiter() *limiter {
	return &limiter{failures: map[string][]time.Time{}, inflight: map[string]int{}, now: time.Now}
}

// limiterKey is the rate-limit key for a client address: the IPv4
// address, or the /64 prefix of an IPv6 address (one host usually owns a
// whole /64, so per-address keys would be trivial to rotate).
func limiterKey(ip string) string {
	p := net.ParseIP(ip)
	if p == nil {
		return ip
	}
	if v4 := p.To4(); v4 != nil {
		return v4.String()
	}
	return p.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// outcome is how an attempt ended.
type outcome int

const (
	// attemptFailed keeps the failure counted (wrong password, refused).
	attemptFailed outcome = iota
	// attemptOK clears every failure of the key.
	attemptOK
	// attemptNeutral takes the attempt back without clearing older
	// failures (internal error, client went away before PAM ran).
	attemptNeutral
)

// attempt is a reserved authentication attempt; done must be called once.
type attempt struct {
	l    *limiter
	key  string
	at   time.Time
	once sync.Once
}

// rejection says why begin refused an attempt.
type rejection struct {
	// wait is how long until the key may try again.
	wait time.Duration
	// busy: too many attempts of this key are running right now.
	busy bool
}

// begin reserves an attempt for key: it is refused when the key already
// reached max failures (counting attempts in flight) or has too many
// attempts running. The reservation is counted as a failure until done.
func (l *limiter) begin(key string, max int) (*attempt, *rejection) {
	if max < 1 {
		max = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(key)
	if len(f) >= max {
		return nil, &rejection{wait: f[len(f)-max].Add(failureWindow).Sub(l.now())}
	}
	if l.inflight[key] >= maxInflightPerKey {
		return nil, &rejection{wait: time.Second, busy: true}
	}
	now := l.now()
	f = append(f, now)
	if len(f) > 1000 {
		f = f[len(f)-1000:]
	}
	l.failures[key] = f
	l.inflight[key]++
	return &attempt{l: l, key: key, at: now}, nil
}

// done ends the attempt.
func (a *attempt) done(o outcome) {
	a.once.Do(func() {
		l := a.l
		l.mu.Lock()
		defer l.mu.Unlock()
		if n := l.inflight[a.key] - 1; n > 0 {
			l.inflight[a.key] = n
		} else {
			delete(l.inflight, a.key)
		}
		switch o {
		case attemptOK:
			delete(l.failures, a.key)
		case attemptNeutral:
			f := l.failures[a.key]
			if i := slices.Index(f, a.at); i >= 0 {
				f = slices.Delete(f, i, i+1)
			}
			if len(f) == 0 {
				delete(l.failures, a.key)
			} else {
				l.failures[a.key] = f
			}
		}
	})
}

// blocked reports whether key reached max failures within the window and,
// if so, when it may try again.
func (l *limiter) blocked(key string, max int) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(key)
	if len(f) < max {
		return false, 0
	}
	return true, f[len(f)-max].Add(failureWindow).Sub(l.now())
}

// prune drops expired entries for key; callers hold mu.
func (l *limiter) prune(key string) []time.Time {
	cut := l.now().Add(-failureWindow)
	f := l.failures[key]
	i := 0
	for i < len(f) && f[i].Before(cut) {
		i++
	}
	f = f[i:]
	if len(f) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = f
	return f
}

// gc drops every expired entry.
func (l *limiter) gc() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.failures {
		l.prune(key)
	}
}
