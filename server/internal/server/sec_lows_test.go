package server

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/bridge"
)

func TestBridgePoolStartsOutsideItsLock(t *testing.T) {
	bp := newBridgePool()
	unblock := make(chan struct{})
	started := make(chan struct{})
	go bp.acquire("u:1", func() (*bridge.Proc, error) {
		close(started)
		<-unblock
		return nil, errors.New("slow failed")
	})
	<-started
	done := make(chan error, 1)
	go func() {
		_, err := bp.acquire("u:2", func() (*bridge.Proc, error) { return nil, errors.New("fast failed") })
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != "fast failed" {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		close(unblock)
		t.Fatal("a bridge of another user waited for the slow one")
	}
	// A second caller for the slow key waits for the same start.
	res := make(chan error, 1)
	go func() {
		_, err := bp.acquire("u:1", func() (*bridge.Proc, error) { return nil, errors.New("second start") })
		res <- err
	}()
	time.Sleep(50 * time.Millisecond)
	close(unblock)
	if err := <-res; err == nil || err.Error() != "second start" {
		t.Fatalf("after the failed start the next caller starts again: %v", err)
	}
}

func TestHTTPApprovalDoesNotAllowWS(t *testing.T) {
	csp := pluginFrameCSP("n", []string{"http://plain.lan:80", "tls.example.org"})
	if strings.Contains(csp, "ws://plain.lan") || !strings.Contains(csp, "http://plain.lan:80") || !strings.Contains(csp, "wss://tls.example.org") {
		t.Fatal(csp)
	}
}
