package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

const (
	maxChannels   = 64
	wsReadLimit   = 2 << 20
	inputQueueLen = 256
)

// wsFrame is a WebSocket message in either direction.
type wsFrame struct {
	Ch     int64           `json:"ch"`
	Op     string          `json:"op"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Admin  bool            `json:"admin,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	B64    bool            `json:"b64,omitempty"`
	Error  *rpc.Error      `json:"error,omitempty"`
}

type wsChannel struct {
	stream *rpc.ClientStream
	admin  bool
	inputs chan json.RawMessage
	failed *rpc.Error // set under wsConn.mu when the daemon ends the channel
}

type wsConn struct {
	s    *Server
	sess *Session
	c    *websocket.Conn
	ctx  context.Context

	mu       sync.Mutex
	channels map[int64]*wsChannel
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request, sess *Session) {
	opts := &websocket.AcceptOptions{}
	if s.opts.Dev {
		opts.OriginPatterns = []string{"localhost:*", "127.0.0.1:*", "[::1]:*"}
	}
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return // Accept already wrote the response
	}
	c.SetReadLimit(wsReadLimit)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	wc := &wsConn{s: s, sess: sess, c: c, ctx: ctx, channels: map[int64]*wsChannel{}}
	go func() {
		select {
		case <-sess.Done():
			c.Close(websocket.StatusPolicyViolation, "session ended")
		case <-ctx.Done():
		}
	}()
	go wc.keepalive()
	err = wc.readLoop()
	cancel()
	wc.closeAll()
	if err != nil && websocket.CloseStatus(err) == -1 && !errors.Is(err, context.Canceled) {
		c.Close(websocket.StatusInternalError, "")
		return
	}
	c.Close(websocket.StatusNormalClosure, "")
}

func (wc *wsConn) keepalive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-wc.ctx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(wc.ctx, 15*time.Second)
			err := wc.c.Ping(ctx)
			cancel()
			if err != nil {
				wc.c.CloseNow()
				return
			}
		}
	}
}

func (wc *wsConn) send(f *wsFrame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(wc.ctx, 30*time.Second)
	defer cancel()
	if err := wc.c.Write(ctx, websocket.MessageText, b); err != nil {
		wc.c.CloseNow()
	}
}

func (wc *wsConn) sendError(ch int64, e *rpc.Error) {
	wc.send(&wsFrame{Ch: ch, Op: "error", Error: e})
}

func (wc *wsConn) readLoop() error {
	for {
		typ, data, err := wc.c.Read(wc.ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		var f wsFrame
		if err := json.Unmarshal(data, &f); err != nil || f.Ch <= 0 {
			wc.sendError(0, rpc.Errorf(rpc.Invalid, "invalid frame"))
			continue
		}
		switch f.Op {
		case "open":
			wc.sess.touch(time.Now())
			wc.open(&f)
		case "input":
			wc.input(&f)
		case "close":
			wc.closeChannel(f.Ch)
		default:
			wc.sendError(f.Ch, rpc.Errorf(rpc.Invalid, "unknown op %q", f.Op))
		}
	}
}

func (wc *wsConn) open(f *wsFrame) {
	wc.mu.Lock()
	_, dup := wc.channels[f.Ch]
	n := len(wc.channels)
	wc.mu.Unlock()
	if dup {
		wc.sendError(f.Ch, rpc.Errorf(rpc.Conflict, "channel %d already open", f.Ch))
		return
	}
	if n >= maxChannels {
		wc.sendError(f.Ch, rpc.Errorf(rpc.Unavailable, "too many open channels"))
		return
	}
	p, isAdmin, e := wc.s.route(wc.ctx, wc.sess, f.Method, f.Admin)
	if e != nil {
		wc.sendError(f.Ch, e)
		return
	}
	release := wc.sess.hold(p, isAdmin)
	st, err := p.Stream(wc.ctx, f.Method, f.Params)
	if err != nil {
		release()
		wc.sendError(f.Ch, rpc.ToError(err, false))
		return
	}
	ch := &wsChannel{stream: st, admin: isAdmin, inputs: make(chan json.RawMessage, inputQueueLen)}
	wc.mu.Lock()
	wc.channels[f.Ch] = ch
	wc.mu.Unlock()

	id := f.Ch
	// Inputs are forwarded by their own goroutine so a full bridge window
	// never blocks the WebSocket reader.
	go func() {
		for in := range ch.inputs {
			if err := st.Send(wc.ctx, in); err != nil {
				return
			}
		}
	}()
	go func() {
		defer release()
		defer st.Close()
		for ev := range st.Events() {
			wc.send(&wsFrame{Ch: id, Op: "data", Data: ev.Data, B64: ev.B64})
		}
		wc.mu.Lock()
		if wc.channels[id] == ch {
			delete(wc.channels, id)
			close(ch.inputs)
		}
		failed := ch.failed
		wc.mu.Unlock()
		if failed != nil {
			wc.sendError(id, failed)
		} else if err := st.Err(); err != nil {
			wc.sendError(id, rpc.ToError(err, false))
		} else {
			wc.send(&wsFrame{Ch: id, Op: "end"})
		}
	}()
}

func (wc *wsConn) input(f *wsFrame) {
	wc.mu.Lock()
	ch := wc.channels[f.Ch]
	if ch == nil {
		wc.mu.Unlock()
		return // stream already ended
	}
	data := f.Data
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	select {
	case ch.inputs <- data:
		wc.mu.Unlock()
	default:
		ch.failed = rpc.Errorf(rpc.Unavailable, "input queue full, channel closed")
		wc.mu.Unlock()
		ch.stream.Close()
		return
	}
	if ch.admin {
		wc.sess.touchAdmin()
	}
}

func (wc *wsConn) closeChannel(id int64) {
	wc.mu.Lock()
	ch := wc.channels[id]
	wc.mu.Unlock()
	if ch != nil {
		ch.stream.Close()
	}
}

func (wc *wsConn) closeAll() {
	wc.mu.Lock()
	chans := make([]*wsChannel, 0, len(wc.channels))
	for _, ch := range wc.channels {
		chans = append(chans, ch)
	}
	wc.mu.Unlock()
	for _, ch := range chans {
		ch.stream.Close()
	}
}
