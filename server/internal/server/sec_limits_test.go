package server

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWSLargeInputFrameIsNotBuffered(t *testing.T) {
	ts, srv := newTestServer(t, true)
	cl := noAuthClient(t, srv, ts)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := dialWS(t, ctx, ts.URL, cookieHeader(cl, ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	big := []byte(`{"ch":1,"op":"input","data":"` + strings.Repeat("a", 10<<20) + `"}`)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_ = c.Write(ctx, websocket.MessageText, big)
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("oversized frame: %v", err)
	}
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 6<<20 {
		t.Fatalf("the daemon allocated %d bytes for a refused 10 MiB frame", got)
	}
}

func TestOpensStream(t *testing.T) {
	for in, want := range map[string]bool{
		`{"ch":1,"op":"open","method":"x","params":{"a":"` + strings.Repeat("b", 100): true,
		`{"ch":1,"method":"x","op":"open","params":{`:                                 true,
		`{"ch":1,"params":{"a":"` + strings.Repeat("b", 100):                          false,
		`{"ch":1,"op":"input","data":"` + strings.Repeat("b", 100):                    false,
		`{"ch":1,"data":{"op":"open"},"op":"input"}`:                                  false,
		`["op","open"]`: false,
	} {
		if got := opensStream([]byte(in)); got != want {
			t.Errorf("%.60s: %v", in, got)
		}
	}
}

func TestLargeBodySlotsPerSession(t *testing.T) {
	s := &Session{}
	var rel []func()
	for i := 0; i < maxBigBodies; i++ {
		r, e := s.acquireBig(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		rel = append(rel, r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, e := s.acquireBig(ctx); e == nil {
		t.Fatal("a third large body got a slot")
	}
	rel[0]()
	rel[0]() // idempotent
	if r, e := s.acquireBig(context.Background()); e != nil {
		t.Fatal(e)
	} else {
		r()
	}
	// The HTTP path takes a slot for a large body: with both taken, a large
	// /api/rpc call waits and gives up with the request.
	ts, srv := newTestServer(t, true)
	cl := noAuthClient(t, srv, ts)
	sess := srv.sessions.all()[0]
	r1, _ := sess.acquireBig(context.Background())
	r2, _ := sess.acquireBig(context.Background())
	defer r1()
	defer r2()
	body := `{"method":"system.info","params":{"pad":"` + strings.Repeat("x", 2<<20) + `"}}`
	rctx, rcancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer rcancel()
	req, _ := http.NewRequestWithContext(rctx, "POST", ts.URL+"/api/rpc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "ervisio")
	if resp, err := cl.Do(req); err == nil {
		resp.Body.Close()
		t.Fatalf("a large call ran without a slot: %d", resp.StatusCode)
	}
}
