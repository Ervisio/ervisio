package rpc

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"sync"
	"testing"
	"time"
)

type pair struct {
	client *Client
	cancel context.CancelFunc
	served chan error
	toSrv  *io.PipeWriter
}

func testRegistry() (*Registry, chan struct{}) {
	r := NewRegistry()
	cancelled := make(chan struct{}, 1)
	r.Handle("t.echo", User, func(ctx context.Context, c *Call) (any, error) {
		var p struct {
			Msg string `json:"msg"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return map[string]string{"msg": p.Msg}, nil
	})
	r.Handle("t.fail", User, func(ctx context.Context, c *Call) (any, error) {
		return nil, Errorf(NotFound, "nothing here")
	})
	r.Handle("t.perm", User, func(ctx context.Context, c *Call) (any, error) {
		return nil, &fs.PathError{Op: "open", Path: "/root", Err: fs.ErrPermission}
	})
	r.Handle("t.panic", User, func(ctx context.Context, c *Call) (any, error) {
		var m map[string]int
		m["x"] = 1
		return nil, nil
	})
	r.Handle("t.root", Admin, func(ctx context.Context, c *Call) (any, error) { return "ok", nil })
	r.Handle("t.block", User, func(ctx context.Context, c *Call) (any, error) {
		<-ctx.Done()
		cancelled <- struct{}{}
		return nil, ctx.Err()
	})
	r.Stream("t.count", User, func(ctx context.Context, c *Call, s Stream) error {
		var p struct{ N int }
		if err := c.Bind(&p); err != nil {
			return err
		}
		for i := 0; i < p.N; i++ {
			if err := s.Send(i); err != nil {
				return err
			}
		}
		return nil
	})
	r.Stream("t.echoStream", User, func(ctx context.Context, c *Call, s Stream) error {
		for {
			select {
			case in, ok := <-s.Input():
				if !ok {
					return ctx.Err()
				}
				var v string
				if err := json.Unmarshal(in, &v); err != nil {
					return Errorf(Invalid, "bad input")
				}
				if v == "quit" {
					return nil
				}
				if err := s.SendBytes([]byte(v)); err != nil {
					return err
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	return r, cancelled
}

func newPair(t *testing.T, reg *Registry, admin bool) *pair {
	t.Helper()
	srvIn, toSrv := io.Pipe()
	fromSrv, srvOut := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	p := &pair{cancel: cancel, served: make(chan error, 1), toSrv: toSrv}
	go func() {
		err := Serve(ctx, reg, srvIn, srvOut, ServeOptions{Admin: admin, UID: 1000, Version: "test", Logger: log.New(io.Discard, "", 0)})
		srvOut.Close()
		p.served <- err
	}()
	p.client = NewClient(fromSrv, toSrv, log.New(io.Discard, "", 0))
	t.Cleanup(func() { p.client.Close(); cancel() })
	return p
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestHello(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, false)
	h, err := p.client.Hello(ctxT(t))
	if err != nil {
		t.Fatal(err)
	}
	if h.UID != 1000 || h.Admin || h.Methods["t.root"] != "admin" || h.Methods["t.echo"] != "user" {
		t.Fatalf("hello %+v", h)
	}
	if lvl, ok := p.client.Level("t.root"); !ok || lvl != Admin {
		t.Fatal("level")
	}
}

func TestCalls(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, false)
	ctx := ctxT(t)
	res, err := p.client.Call(ctx, "t.echo", map[string]string{"msg": "hi"})
	if err != nil || string(res) != `{"msg":"hi"}` {
		t.Fatalf("echo: %s %v", res, err)
	}
	cases := map[string]Code{"t.fail": NotFound, "t.perm": NeedsAdmin, "t.panic": Internal, "t.root": NeedsAdmin, "t.nope": NotFound, "t.count": Invalid}
	for m, code := range cases {
		_, err := p.client.Call(ctx, m, nil)
		if !IsCode(err, code) {
			t.Errorf("%s: got %v want %s", m, err, code)
		}
	}
	_, err = p.client.Call(ctx, "t.echo", json.RawMessage(`{"msg":5}`))
	if !IsCode(err, Invalid) {
		t.Errorf("bind: %v", err)
	}
}

func TestAdminBridge(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, true)
	ctx := ctxT(t)
	if res, err := p.client.Call(ctx, "t.root", nil); err != nil || string(res) != `"ok"` {
		t.Fatalf("%s %v", res, err)
	}
	if _, err := p.client.Call(ctx, "t.perm", nil); !IsCode(err, Forbidden) {
		t.Fatalf("perm on admin: %v", err)
	}
}

func TestConcurrentAndCancel(t *testing.T) {
	reg, cancelled := testRegistry()
	p := newPair(t, reg, false)
	ctx := ctxT(t)
	bctx, bcancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := p.client.Call(bctx, "t.block", nil); done <- err }()
	// A blocked call must not delay others.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.client.Call(ctx, "t.echo", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	bcancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("blocked call: %v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("handler context not cancelled")
	}
}

func TestStreamFlowControl(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, false)
	ctx := ctxT(t)
	const n = 1000
	st, err := p.client.Stream(ctx, "t.count", map[string]int{"N": n})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// While the consumer is idle the bridge must still answer calls.
	time.Sleep(50 * time.Millisecond)
	if _, err := p.client.Call(ctx, "t.echo", nil); err != nil {
		t.Fatal(err)
	}
	i := 0
	for ev := range st.Events() {
		var v int
		json.Unmarshal(ev.Data, &v)
		if v != i {
			t.Fatalf("event %d: got %d", i, v)
		}
		i++
	}
	if i != n || st.Err() != nil {
		t.Fatalf("got %d events, err %v", i, st.Err())
	}
}

func TestStreamInputAndCancel(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, false)
	ctx := ctxT(t)
	st, err := p.client.Stream(ctx, "t.echoStream", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if err := st.Send(ctx, json.RawMessage(`"ab"`)); err != nil {
			t.Fatal(err)
		}
		ev := <-st.Events()
		if !ev.B64 || string(ev.Data) != `"YWI="` {
			t.Fatalf("event %+v", ev)
		}
	}
	st.Send(ctx, json.RawMessage(`"quit"`))
	if _, ok := <-st.Events(); ok {
		t.Fatal("expected end")
	}
	if st.Err() != nil {
		t.Fatal(st.Err())
	}
	st.Close()

	// Cancel from the client side.
	st2, _ := p.client.Stream(ctx, "t.echoStream", nil)
	st2.Close()
	for range st2.Events() {
	}
	// Bridge still healthy.
	if _, err := p.client.Call(ctx, "t.echo", nil); err != nil {
		t.Fatal(err)
	}
	// Stream errors propagate.
	st3, _ := p.client.Stream(ctx, "t.echoStream", nil)
	defer st3.Close()
	st3.Send(ctx, json.RawMessage(`5`))
	for range st3.Events() {
	}
	if !IsCode(st3.Err(), Invalid) {
		t.Fatalf("stream err %v", st3.Err())
	}
}

func TestBridgeDeath(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, false)
	ctx := ctxT(t)
	if _, err := p.client.Hello(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := p.client.Call(ctx, "t.block", nil); done <- err }()
	st, _ := p.client.Stream(ctx, "t.echoStream", nil)
	defer st.Close()
	time.Sleep(20 * time.Millisecond)
	p.cancel() // the bridge goes away
	if err := <-done; !IsCode(err, Unavailable) {
		t.Fatalf("pending call: %v", err)
	}
	for range st.Events() {
	}
	if !IsCode(st.Err(), Unavailable) {
		t.Fatalf("stream: %v", st.Err())
	}
	<-p.client.Done()
	if _, err := p.client.Call(ctx, "t.echo", nil); !IsCode(err, Unavailable) {
		t.Fatalf("after death: %v", err)
	}
}

func TestServerEOF(t *testing.T) {
	reg, _ := testRegistry()
	p := newPair(t, reg, false)
	p.client.Hello(ctxT(t))
	p.client.Close()
	select {
	case err := <-p.served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return on EOF")
	}
}

func TestLineReader(t *testing.T) {
	r, w := io.Pipe()
	go func() {
		w.Write([]byte("short\n"))
		big := make([]byte, 300)
		for i := range big {
			big[i] = 'x'
		}
		w.Write(append(big, '\n'))
		w.Write([]byte("after"))
		w.Close()
	}()
	lr := newLineReader(r, 100)
	if l, err := lr.next(); err != nil || string(l) != "short\n" {
		t.Fatalf("%q %v", l, err)
	}
	if _, err := lr.next(); err != errLineTooLong {
		t.Fatalf("want too long, got %v", err)
	}
	if l, err := lr.next(); err != nil || string(l) != "after" {
		t.Fatalf("%q %v", l, err)
	}
}

func TestValidMethodName(t *testing.T) {
	for s, want := range map[string]bool{"a.b": true, "system.metricsStream": true, "x": false, ".a": false, "a.": false, "a b.c": false, "a.b;rm": false, "1a.b": false} {
		if ValidMethodName(s) != want {
			t.Errorf("%q", s)
		}
	}
}
