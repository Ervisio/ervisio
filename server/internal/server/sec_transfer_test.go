package server

import (
	"testing"
	"time"
)

func TestTransfersEndWithTheSession(t *testing.T) {
	e := newXferEnv(t)
	_, r := e.start(t, map[string]any{"kind": "download", "name": "svc", "method": "GET", "path": "/dl/chunked", "filename": "x"})
	if r["url"] == nil {
		t.Fatalf("%v", r)
	}
	sess := e.srv.sessions.all()[0]
	e.srv.sessions.remove(sess)
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.srv.transfers.mu.Lock()
		n, live := len(e.srv.transfers.byID), e.srv.transfers.live[sess]
		e.srv.transfers.mu.Unlock()
		if n == 0 && live == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d transfers still waiting, %d held after the session ended", n, live)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
