package server

import (
	"sync"
	"time"
)

// failureWindow is how long failed sign-ins are remembered per client IP.
const failureWindow = 15 * time.Minute

// limiter counts failed authentications per client IP.
type limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	now      func() time.Time
}

func newLimiter() *limiter {
	return &limiter{failures: map[string][]time.Time{}, now: time.Now}
}

// blocked reports whether ip reached max failures within the window and,
// if so, when it may try again.
func (l *limiter) blocked(ip string, max int) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.prune(ip)
	if len(f) < max {
		return false, 0
	}
	return true, f[len(f)-max].Add(failureWindow).Sub(l.now())
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := append(l.prune(ip), l.now())
	if len(f) > 1000 {
		f = f[len(f)-1000:]
	}
	l.failures[ip] = f
}

func (l *limiter) reset(ip string) {
	l.mu.Lock()
	delete(l.failures, ip)
	l.mu.Unlock()
}

// prune drops expired entries for ip; callers hold mu.
func (l *limiter) prune(ip string) []time.Time {
	cut := l.now().Add(-failureWindow)
	f := l.failures[ip]
	i := 0
	for i < len(f) && f[i].Before(cut) {
		i++
	}
	f = f[i:]
	if len(f) == 0 {
		delete(l.failures, ip)
		return nil
	}
	l.failures[ip] = f
	return f
}

// gc drops every expired entry.
func (l *limiter) gc() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ip := range l.failures {
		l.prune(ip)
	}
}
