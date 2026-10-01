package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestSessionWSAccounting(t *testing.T) {
	sess := &Session{done: make(chan struct{})}
	for i := 0; i < maxSessionConns; i++ {
		if !sess.reserveConn() {
			t.Fatalf("conn %d refused", i)
		}
	}
	if sess.reserveConn() {
		t.Fatal("conn over the limit accepted")
	}
	sess.releaseConn()
	if !sess.reserveConn() {
		t.Fatal("released slot not reusable")
	}
	for i := 0; i < maxSessionChannels; i++ {
		if !sess.reserveChannel() {
			t.Fatalf("channel %d refused", i)
		}
	}
	if sess.reserveChannel() {
		t.Fatal("channel over the limit accepted")
	}
	if !sess.chargeInput(sessionInputBudget) || sess.chargeInput(1) {
		t.Fatal("input budget")
	}
	sess.chargeInput(-sessionInputBudget)
	if !sess.chargeInput(1) {
		t.Fatal("budget not given back")
	}
}

func dialWS(t *testing.T, ctx context.Context, url string, h http.Header) (*websocket.Conn, error) {
	t.Helper()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/api/ws", &websocket.DialOptions{HTTPHeader: h})
	return c, err
}

func TestWSLimits(t *testing.T) {
	ts, srv := newTestServer(t, true)
	cl := noAuthClient(t, srv, ts)
	h := cookieHeader(cl, ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// A frame over wsReadLimit closes the connection (1009).
	c, err := dialWS(t, ctx, ts.URL, h)
	if err != nil {
		t.Fatal(err)
	}
	big := `{"ch":1,"op":"input","data":"` + strings.Repeat("a", wsReadLimit) + `"}`
	_ = c.Write(ctx, websocket.MessageText, []byte(big))
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
		t.Fatalf("oversized frame: %v", err)
	}
	c.CloseNow()

	// Inputs to a stream that never reads them: the bounded queues fill and
	// the channel ends with an error instead of buffering without limit.
	c, err = dialWS(t, ctx, ts.URL, h)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.Write(ctx, websocket.MessageText, []byte(`{"ch":5,"op":"open","method":"system.metricsStream","params":{"interval":60000}}`))
	in := []byte(`{"ch":5,"op":"input","data":"` + strings.Repeat("x", 1000) + `"}`)
	for i := 0; i < 4*inputQueueLen; i++ {
		if err := c.Write(ctx, websocket.MessageText, in); err != nil {
			t.Fatal(err)
		}
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var f wsFrame
		json.Unmarshal(data, &f)
		if f.Ch == 5 && f.Op == "error" {
			if f.Error == nil || f.Error.Code != "unavailable" {
				t.Fatalf("frame %s", data)
			}
			break
		}
		if f.Op == "end" {
			t.Fatalf("stream ended without the queue error: %s", data)
		}
	}
	// Budget and channel slots are given back once the channel is gone.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var sess *Session
		for _, s := range srv.sessions.all() {
			sess = s
		}
		sess.mu.Lock()
		bytes, chans := sess.inputBytes, sess.wsChannels
		sess.mu.Unlock()
		if bytes == 0 && chans == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("leaked accounting: %d bytes, %d channels", bytes, chans)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Connections per session are capped.
	var conns []*websocket.Conn
	defer func() {
		for _, x := range conns {
			x.CloseNow()
		}
	}()
	for i := 0; i < maxSessionConns-1; i++ { // c is still open
		x, err := dialWS(t, ctx, ts.URL, h)
		if err != nil {
			t.Fatalf("conn %d: %v", i, err)
		}
		conns = append(conns, x)
	}
	if x, err := dialWS(t, ctx, ts.URL, h); err == nil {
		x.CloseNow()
		t.Fatal("connection over the per-session limit accepted")
	}
}

func TestDevHostCheck(t *testing.T) {
	ts, _ := newTestServer(t, false)
	req, _ := http.NewRequest("GET", ts.URL+"/api/public/host", nil)
	req.Host = "rebind.example:9090"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("non-loopback Host: %d", resp.StatusCode)
	}
	req.Host = "localhost:9090"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("localhost Host: %d", resp.StatusCode)
	}
}
