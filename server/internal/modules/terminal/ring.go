package terminal

import "sync"

// ScrollbackSize is how much output a session keeps for replay on attach.
const ScrollbackSize = 256 << 10

// ring is a fixed-size byte ring buffer that keeps the most recent output.
type ring struct {
	mu   sync.Mutex
	buf  []byte
	pos  int // next write position
	full bool
}

func newRing(size int) *ring { return &ring{buf: make([]byte, size)} }

// Write appends p, dropping the oldest bytes when full. It never fails.
func (r *ring) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if len(p) >= len(r.buf) {
		copy(r.buf, p[len(p)-len(r.buf):])
		r.pos, r.full = 0, true
		return n, nil
	}
	c := copy(r.buf[r.pos:], p)
	if c < len(p) {
		copy(r.buf, p[c:])
		r.full = true
	}
	r.pos = (r.pos + len(p)) % len(r.buf)
	if r.pos == 0 && len(p) > 0 {
		r.full = true
	}
	return n, nil
}

// Snapshot returns a copy of the buffered output, oldest byte first.
func (r *ring) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *ring) snapshotLocked() []byte {
	if !r.full {
		return append([]byte(nil), r.buf[:r.pos]...)
	}
	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.pos:]...)
	return append(out, r.buf[:r.pos]...)
}
