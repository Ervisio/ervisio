package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// ServeOptions describe the bridge in its hello line.
type ServeOptions struct {
	Admin   bool
	UID     int
	Version string
	// Logger receives protocol diagnostics; nil uses log.Default().
	Logger *log.Logger
}

type active struct {
	cancel context.CancelFunc
	stream *serverStream // nil for plain calls
}

type server struct {
	reg  *Registry
	opts ServeOptions
	out  *lineWriter
	log  *log.Logger

	mu     sync.Mutex
	active map[uint64]*active
	wg     sync.WaitGroup
}

// Serve runs the bridge protocol on in/out until in reaches EOF or ctx is
// cancelled. It writes the hello line first, runs every request in its own
// goroutine and cancels all of them when it returns.
func Serve(ctx context.Context, reg *Registry, in io.Reader, out io.Writer, opts ServeOptions) error {
	s := &server{reg: reg, opts: opts, out: &lineWriter{w: out}, log: opts.Logger, active: map[uint64]*active{}}
	if s.log == nil {
		s.log = log.Default()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		s.wg.Wait()
	}()
	if err := s.out.write(&Message{Hello: reg.hello(opts.Admin, opts.UID, opts.Version)}); err != nil {
		return err
	}

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		lr := newLineReader(in, MaxLine)
		for {
			line, err := lr.next()
			if errors.Is(err, errLineTooLong) {
				s.log.Printf("rpc: dropped line longer than %d bytes", MaxLine)
				continue
			}
			if err != nil {
				readErr <- err
				return
			}
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			if err == io.EOF {
				return nil
			}
			return err
		case line := <-lines:
			var m Message
			if err := json.Unmarshal(line, &m); err != nil {
				s.log.Printf("rpc: invalid message: %v", err)
				continue
			}
			s.dispatch(ctx, &m)
		}
	}
}

func (s *server) dispatch(ctx context.Context, m *Message) {
	if m.ID == 0 {
		s.log.Printf("rpc: message without id ignored")
		return
	}
	if m.Method == "" {
		s.control(m)
		return
	}
	reply := func(e *Error) { _ = s.out.write(&Message{ID: m.ID, Error: e}) }

	meth, ok := s.reg.methods[m.Method]
	switch {
	case !ok:
		reply(Errorf(NotFound, "unknown method %s", m.Method))
		return
	case meth.level == Admin && !s.opts.Admin:
		reply(Errorf(NeedsAdmin, "%s needs administrator rights", m.Method))
		return
	case m.Stream && meth.stream == nil:
		reply(Errorf(Invalid, "%s is not a stream", m.Method))
		return
	case !m.Stream && meth.stream != nil:
		reply(Errorf(Invalid, "%s is a stream", m.Method))
		return
	}

	cctx, cancel := context.WithCancel(ctx)
	a := &active{cancel: cancel}
	if m.Stream {
		a.stream = newServerStream(cctx, m.ID, s.out)
	}
	s.mu.Lock()
	if _, dup := s.active[m.ID]; dup {
		s.mu.Unlock()
		cancel()
		reply(Errorf(Invalid, "request id %d already in use", m.ID))
		return
	}
	s.active[m.ID] = a
	s.mu.Unlock()

	call := &Call{ID: m.ID, Method: m.Method, Params: m.Params, Admin: s.opts.Admin}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.active, m.ID)
			s.mu.Unlock()
			cancel()
		}()
		if m.Stream {
			s.runStream(cctx, call, meth.stream, a.stream)
		} else {
			s.runCall(cctx, call, meth.call)
		}
	}()
}

// control handles input / cancel / ack messages for an active request.
func (s *server) control(m *Message) {
	s.mu.Lock()
	a := s.active[m.ID]
	s.mu.Unlock()
	if a == nil {
		return // already finished; late messages are normal
	}
	if m.Cancel {
		if a.stream != nil {
			a.stream.clientCancelled.Store(true)
		}
		a.cancel()
		return
	}
	if a.stream == nil {
		return
	}
	if m.Ack > 0 {
		a.stream.credits.release(m.Ack)
	}
	if m.Input != nil && !a.stream.inputs.push(m.Input) {
		s.log.Printf("rpc: stream %d: input window exceeded, cancelling", m.ID)
		a.stream.fail(Errorf(Invalid, "input flow-control window exceeded"))
		a.cancel()
	}
}

func (s *server) recoverTo(method string, errp *error) {
	if r := recover(); r != nil {
		s.log.Printf("rpc: panic in %s: %v\n%s", method, r, debug.Stack())
		*errp = Errorf(Internal, "internal error in %s", method)
	}
}

func (s *server) runCall(ctx context.Context, c *Call, h Handler) {
	var res any
	err := func() (err error) {
		defer s.recoverTo(c.Method, &err)
		res, err = h(ctx, c)
		return err
	}()
	if err != nil {
		_ = s.out.write(&Message{ID: c.ID, Error: ToError(err, s.opts.Admin)})
		return
	}
	raw, merr := json.Marshal(res)
	if merr != nil {
		_ = s.out.write(&Message{ID: c.ID, Error: Errorf(Internal, "encode result: %v", merr)})
		return
	}
	_ = s.out.write(&Message{ID: c.ID, Result: raw})
}

func (s *server) runStream(ctx context.Context, c *Call, h StreamHandler, st *serverStream) {
	err := func() (err error) {
		defer s.recoverTo(c.Method, &err)
		return h(ctx, c, st)
	}()
	st.finish(err, s.opts.Admin)
}

type serverStream struct {
	ctx     context.Context
	id      uint64
	out     *lineWriter
	credits *credits
	inputs  *inbox[json.RawMessage]

	clientCancelled atomic.Bool

	mu     sync.Mutex
	ended  bool
	failed *Error
}

func newServerStream(ctx context.Context, id uint64, out *lineWriter) *serverStream {
	st := &serverStream{ctx: ctx, id: id, out: out, credits: newCredits()}
	st.inputs = newInbox[json.RawMessage](ctx, func(n int) {
		_ = out.write(&Message{ID: id, Ack: n})
	})
	return st
}

func (st *serverStream) Input() <-chan json.RawMessage { return st.inputs.out }

func (st *serverStream) Send(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	return st.send(raw, false)
}

func (st *serverStream) SendBytes(b []byte) error {
	raw, _ := json.Marshal(base64.StdEncoding.EncodeToString(b))
	return st.send(raw, true)
}

func (st *serverStream) send(raw json.RawMessage, b64 bool) error {
	if err := st.credits.acquire(st.ctx); err != nil {
		return err
	}
	st.mu.Lock()
	ended := st.ended
	st.mu.Unlock()
	if ended {
		return context.Canceled
	}
	return st.out.write(&Message{ID: st.id, Event: EventData, Data: raw, B64: b64})
}

// fail records an error that ends the stream regardless of the handler's
// own return value.
func (st *serverStream) fail(e *Error) {
	st.mu.Lock()
	if st.failed == nil {
		st.failed = e
	}
	st.mu.Unlock()
}

func (st *serverStream) finish(err error, admin bool) {
	st.mu.Lock()
	if st.ended {
		st.mu.Unlock()
		return
	}
	st.ended = true
	failed := st.failed
	st.mu.Unlock()
	switch {
	case failed != nil:
		_ = st.out.write(&Message{ID: st.id, Error: failed})
	case st.clientCancelled.Load():
		_ = st.out.write(&Message{ID: st.id, Event: EventEnd})
	case st.ctx.Err() != nil:
		_ = st.out.write(&Message{ID: st.id, Error: Errorf(Unavailable, "bridge shutting down")})
	case err != nil:
		_ = st.out.write(&Message{ID: st.id, Error: ToError(err, admin)})
	default:
		_ = st.out.write(&Message{ID: st.id, Event: EventEnd})
	}
}
