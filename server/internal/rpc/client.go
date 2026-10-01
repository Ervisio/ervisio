package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"sync/atomic"
)

// ErrBridgeGone is returned for calls on a bridge that has exited.
var ErrBridgeGone = Errorf(Unavailable, "bridge process exited")

// Event is one stream event received from a bridge.
type Event struct {
	Data json.RawMessage
	B64  bool
}

// Client is the daemon side of a bridge connection.
type Client struct {
	out    *lineWriter
	closer io.Closer
	log    *log.Logger

	nextID atomic.Uint64

	helloOnce sync.Once
	helloCh   chan struct{}
	hello     *Hello

	mu      sync.Mutex
	pending map[uint64]chan *Message
	streams map[uint64]*ClientStream
	dead    bool

	done chan struct{}
	err  error
}

// NewClient starts reading bridge messages from r; requests are written to w.
// Closing the client closes w, which makes the bridge exit.
func NewClient(r io.Reader, w io.WriteCloser, logger *log.Logger) *Client {
	if logger == nil {
		logger = log.Default()
	}
	c := &Client{
		out:     &lineWriter{w: w},
		closer:  w,
		log:     logger,
		helloCh: make(chan struct{}),
		pending: map[uint64]chan *Message{},
		streams: map[uint64]*ClientStream{},
		done:    make(chan struct{}),
	}
	go c.readLoop(r)
	return c
}

// Hello waits for the bridge's hello line.
func (c *Client) Hello(ctx context.Context) (*Hello, error) {
	select {
	case <-c.helloCh:
		if c.hello == nil {
			return nil, c.Err()
		}
		return c.hello, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Level returns the level of method as announced in the hello line.
func (c *Client) Level(method string) (Level, bool) {
	select {
	case <-c.helloCh:
	default:
		return User, false
	}
	if c.hello == nil {
		return User, false
	}
	s, ok := c.hello.Methods[method]
	if !ok {
		return User, false
	}
	return ParseLevel(s)
}

// Done is closed when the bridge connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Alive reports whether the bridge connection is still up.
func (c *Client) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Err returns why the connection ended (nil while alive).
func (c *Client) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

// Close closes the request pipe; the bridge exits on EOF.
func (c *Client) Close() error { return c.closer.Close() }

func (c *Client) readLoop(r io.Reader) {
	lr := newLineReader(r, MaxLine)
	var err error
	for {
		var line []byte
		line, err = lr.next()
		if errors.Is(err, errLineTooLong) {
			c.log.Printf("rpc client: dropped overlong line from bridge")
			continue
		}
		if err != nil {
			break
		}
		var m Message
		if jerr := json.Unmarshal(line, &m); jerr != nil {
			c.log.Printf("rpc client: invalid message from bridge: %v", jerr)
			continue
		}
		if m.Hello != nil {
			c.helloOnce.Do(func() {
				c.hello = m.Hello
				close(c.helloCh)
			})
			continue
		}
		c.dispatch(&m)
	}
	c.shutdown(err)
}

func (c *Client) shutdown(err error) {
	c.mu.Lock()
	c.dead = true
	if err == nil || err == io.EOF {
		c.err = ErrBridgeGone
	} else {
		c.err = Errorf(Unavailable, "bridge connection: %v", err)
	}
	pending := c.pending
	streams := c.streams
	c.pending = map[uint64]chan *Message{}
	c.streams = map[uint64]*ClientStream{}
	c.mu.Unlock()
	c.helloOnce.Do(func() { close(c.helloCh) })
	for _, ch := range pending {
		ch <- &Message{Error: ErrBridgeGone}
	}
	for _, s := range streams {
		s.end(ErrBridgeGone)
	}
	close(c.done)
}

func (c *Client) dispatch(m *Message) {
	c.mu.Lock()
	ch, isCall := c.pending[m.ID]
	if isCall {
		delete(c.pending, m.ID)
	}
	st := c.streams[m.ID]
	c.mu.Unlock()
	switch {
	case isCall:
		ch <- m
	case st != nil:
		switch {
		case m.Error != nil:
			st.end(m.Error)
		case m.Event == EventEnd:
			st.end(nil)
		case m.Event == EventData:
			if !st.events.push(Event{Data: m.Data, B64: m.B64}) {
				c.log.Printf("rpc client: stream %d exceeded the window, cancelling", m.ID)
				st.end(Errorf(Internal, "bridge exceeded the flow-control window"))
				_ = c.out.write(&Message{ID: m.ID, Cancel: true})
			}
		case m.Ack > 0:
			st.credits.release(m.Ack)
		}
	}
	// Messages for unknown ids belong to calls that were cancelled.
}

func (c *Client) register(id uint64, ch chan *Message, st *ClientStream) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dead {
		return ErrBridgeGone
	}
	if ch != nil {
		c.pending[id] = ch
	}
	if st != nil {
		c.streams[id] = st
	}
	return nil
}

func (c *Client) forget(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	delete(c.streams, id)
	c.mu.Unlock()
}

func encodeParams(params any) (json.RawMessage, error) {
	switch p := params.(type) {
	case nil:
		return json.RawMessage("{}"), nil
	case json.RawMessage:
		if len(p) == 0 {
			return json.RawMessage("{}"), nil
		}
		return p, nil
	default:
		b, err := json.Marshal(p)
		if err != nil {
			return nil, Errorf(Invalid, "encode params: %v", err)
		}
		return b, nil
	}
}

// Call performs a request and returns the raw result. Remote failures are
// returned as *Error. Cancelling ctx sends a cancel to the bridge.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, err := encodeParams(params)
	if err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	ch := make(chan *Message, 1)
	if err := c.register(id, ch, nil); err != nil {
		return nil, err
	}
	if err := c.out.write(&Message{ID: id, Method: method, Params: raw}); err != nil {
		c.forget(id)
		return nil, Errorf(Unavailable, "write to bridge: %v", err)
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return nil, m.Error
		}
		return rawOrNull(m.Result), nil
	case <-ctx.Done():
		c.forget(id)
		_ = c.out.write(&Message{ID: id, Cancel: true})
		return nil, ctx.Err()
	}
}

// ClientStream is the daemon side of a streaming call.
type ClientStream struct {
	c       *Client
	id      uint64
	events  *inbox[Event]
	credits *credits
	cancel  context.CancelFunc
	ctx     context.Context

	once sync.Once
	err  error
	done chan struct{}
}

// Stream opens a streaming call. Events arrive on Events() until it is
// closed; then Err() tells whether the stream failed. Cancelling ctx or
// calling Close cancels the stream on the bridge.
func (c *Client) Stream(ctx context.Context, method string, params any) (*ClientStream, error) {
	raw, err := encodeParams(params)
	if err != nil {
		return nil, err
	}
	id := c.nextID.Add(1)
	sctx, cancel := context.WithCancel(ctx)
	st := &ClientStream{c: c, id: id, credits: newCredits(), cancel: cancel, ctx: sctx, done: make(chan struct{})}
	st.events = newInbox[Event](sctx, func(n int) { _ = c.out.write(&Message{ID: id, Ack: n}) })
	if err := c.register(id, nil, st); err != nil {
		cancel()
		return nil, err
	}
	if err := c.out.write(&Message{ID: id, Method: method, Params: raw, Stream: true}); err != nil {
		c.forget(id)
		cancel()
		return nil, Errorf(Unavailable, "write to bridge: %v", err)
	}
	go func() {
		select {
		case <-st.done:
		case <-sctx.Done():
			// Cancelled by the caller before the bridge ended it.
			select {
			case <-st.done:
			default:
				c.forget(id)
				_ = c.out.write(&Message{ID: id, Cancel: true})
				st.end(nil)
			}
		}
	}()
	return st, nil
}

// Events delivers stream events; it is closed when the stream ends.
func (s *ClientStream) Events() <-chan Event { return s.events.out }

// Err is the stream's terminal error, valid after Events() is closed.
func (s *ClientStream) Err() error {
	<-s.done
	return s.err
}

// Send writes one input message (any JSON value) to the stream, blocking
// while the flow-control window is full.
func (s *ClientStream) Send(ctx context.Context, input json.RawMessage) error {
	if len(input) == 0 {
		input = json.RawMessage("null")
	}
	ctx, stop := mergeDone(ctx, s.ctx)
	defer stop()
	if err := s.credits.acquire(ctx); err != nil {
		return err
	}
	select {
	case <-s.done:
		if s.err != nil {
			return s.err
		}
		return context.Canceled
	default:
	}
	return s.c.out.write(&Message{ID: s.id, Input: input})
}

// Close cancels the stream (if still running) and releases its resources.
// Callers must always call Close, typically with defer.
func (s *ClientStream) Close() { s.cancel() }

func (s *ClientStream) end(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.done)
		s.c.forget(s.id)
		s.events.close()
	})
}

// mergeDone returns a context cancelled when either a or b is done.
func mergeDone(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	stop := context.AfterFunc(b, cancel)
	return ctx, func() { stop(); cancel() }
}
