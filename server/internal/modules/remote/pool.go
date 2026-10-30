package remote

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const (
	idleClose = 5 * time.Minute
	// maxOpen bounds the open connections of one session.
	maxOpen = 24
	// waitSlot is how long a call waits for a busy FTP connection.
	waitSlot = 2 * time.Minute
)

// shared reports whether one connection serves calls in parallel (SFTP,
// SMB and HTTP multiplex requests; an FTP control connection does not).
func shared(kind string) bool { return kind != KindFTP }

func maxSlots(kind string) int {
	if shared(kind) {
		return 1
	}
	return 4
}

type slot struct {
	fs    FS
	inUse int
	last  time.Time
}

type entry struct {
	spec    *Conn
	slots   []*slot
	dialing int
	notify  chan struct{} // closed and replaced when a slot is released
}

// pool keeps connections open between calls and closes them after
// idleClose without use. A connection whose saved settings changed (in this
// or another session) is replaced.
type pool struct {
	st   *store
	dial func(ctx context.Context, spec *Conn) (FS, error)

	mu      sync.Mutex
	entries map[string]*entry
	open    int
	reaper  bool
}

func newPool(st *store) *pool {
	return &pool{st: st, dial: dialFS, entries: map[string]*entry{}}
}

func dialFS(ctx context.Context, spec *Conn) (FS, error) {
	switch spec.Kind {
	case KindSFTP:
		return dialSFTP(ctx, spec)
	case KindFTP:
		return dialFTP(ctx, spec)
	case KindSMB:
		return dialSMB(ctx, spec)
	case KindWebDAV:
		return dialWebDAV(ctx, spec)
	}
	return nil, rpc.Errorf(rpc.Invalid, "Unknown protocol %q.", spec.Kind)
}

// handle is one borrowed connection.
type handle struct {
	FS
	spec  *Conn
	fresh bool // dialled for this call
	p     *pool
	id    string
	s     *slot
	done  bool
}

// release gives the connection back. A connection that failed at the
// transport level is closed instead.
func (h *handle) release(err error) {
	if h.done {
		return
	}
	h.done = true
	h.p.put(h.id, h.s, err)
}

func (p *pool) get(ctx context.Context, id string) (*handle, error) {
	if !validID(id) {
		return nil, rpc.Errorf(rpc.Invalid, "The connection id is not valid.")
	}
	spec, err := p.st.get(id)
	if err != nil {
		return nil, err
	}
	deadline := time.NewTimer(waitSlot)
	defer deadline.Stop()
	for {
		p.mu.Lock()
		e := p.entries[id]
		if e != nil && e.spec.Updated != spec.Updated {
			// Edited: close what is idle, forget the rest once released.
			p.dropLocked(id)
			e = nil
		}
		if e == nil {
			e = &entry{spec: spec, notify: make(chan struct{})}
			p.entries[id] = e
		}
		for _, s := range e.slots {
			if shared(spec.Kind) || s.inUse == 0 {
				s.inUse++
				s.last = time.Now()
				p.mu.Unlock()
				return &handle{FS: s.fs, spec: spec, p: p, id: id, s: s}, nil
			}
		}
		if len(e.slots)+e.dialing < maxSlots(spec.Kind) {
			if p.open >= maxOpen {
				p.closeIdleLocked(0)
			}
			if p.open >= maxOpen {
				p.mu.Unlock()
				return nil, rpc.Errorf(rpc.Unavailable, "Too many remote connections are open in this session. Close some tabs and try again.")
			}
			e.dialing++
			p.open++
			p.mu.Unlock()
			f, err := p.dial(ctx, spec)
			p.mu.Lock()
			e.dialing--
			if err != nil {
				p.open--
				p.wakeLocked(e)
				p.mu.Unlock()
				return nil, err
			}
			s := &slot{fs: f, inUse: 1, last: time.Now()}
			if p.entries[id] == e {
				e.slots = append(e.slots, s)
			}
			p.startReaperLocked()
			p.wakeLocked(e)
			p.mu.Unlock()
			return &handle{FS: f, spec: spec, fresh: true, p: p, id: id, s: s}, nil
		}
		ch := e.notify
		p.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, rpc.Errorf(rpc.Unavailable, "The connection is busy with other transfers. Try again when they are done.")
		}
	}
}

func (p *pool) wakeLocked(e *entry) {
	close(e.notify)
	e.notify = make(chan struct{})
}

func (p *pool) put(id string, s *slot, err error) {
	broken := connBroken(err)
	if !broken && err != nil {
		if a, ok := s.fs.(interface{ alive() bool }); ok && !a.alive() {
			broken = true
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s.inUse--
	s.last = time.Now()
	e := p.entries[id]
	member := false
	if e != nil {
		for _, x := range e.slots {
			if x == s {
				member = true
			}
		}
	}
	if broken || !member {
		if member {
			p.removeSlotLocked(e, s)
		}
		if s.inUse <= 0 {
			go s.fs.Close()
			p.open--
		}
	}
	if e != nil {
		p.wakeLocked(e)
	}
}

func (p *pool) removeSlotLocked(e *entry, s *slot) {
	for i, x := range e.slots {
		if x == s {
			e.slots = append(e.slots[:i], e.slots[i+1:]...)
			return
		}
	}
}

// dropLocked forgets a connection: idle slots close now, busy ones when
// their caller releases them.
func (p *pool) dropLocked(id string) {
	e := p.entries[id]
	if e == nil {
		return
	}
	delete(p.entries, id)
	for _, s := range e.slots {
		if s.inUse <= 0 {
			go s.fs.Close()
			p.open--
		}
	}
	e.slots = nil
	p.wakeLocked(e)
}

func (p *pool) drop(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dropLocked(id)
}

// closeIdleLocked closes slots unused for at least age.
func (p *pool) closeIdleLocked(age time.Duration) {
	now := time.Now()
	for id, e := range p.entries {
		keep := e.slots[:0]
		for _, s := range e.slots {
			if s.inUse <= 0 && now.Sub(s.last) >= age {
				go s.fs.Close()
				p.open--
				continue
			}
			keep = append(keep, s)
		}
		e.slots = keep
		if len(e.slots) == 0 && e.dialing == 0 {
			delete(p.entries, id)
		}
	}
}

func (p *pool) startReaperLocked() {
	if p.reaper {
		return
	}
	p.reaper = true
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			p.mu.Lock()
			p.closeIdleLocked(idleClose)
			if len(p.entries) == 0 {
				p.reaper = false
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()
		}
	}()
}

// connBroken reports transport failures (as opposed to "not found" and the
// like), after which a connection is not used again.
func connBroken(err error) bool {
	if err == nil {
		return false
	}
	var re *rpc.Error
	if errors.As(err, &re) {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrExist) || errors.Is(err, fs.ErrPermission) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The call was given up half-way: an FTP transfer may still run.
		return true
	}
	var ne net.Error
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, sftp.ErrSSHFxConnectionLost) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.As(err, &ne)
}

// with runs fn on a pooled connection. A call on a reused connection that
// fails at the transport level is retried once on a new one (the server
// may have dropped an idle connection).
func (p *pool) with(ctx context.Context, id string, fn func(h *handle) error) error {
	for attempt := 0; ; attempt++ {
		h, err := p.get(ctx, id)
		if err != nil {
			return err
		}
		err = fn(h)
		h.release(err)
		if err != nil && attempt == 0 && !h.fresh && connBroken(err) && ctx.Err() == nil {
			continue
		}
		return err
	}
}
