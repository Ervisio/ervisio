package software

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAptReadGateSerializesConcurrentReadsWithoutDroppingResults(t *testing.T) {
	gate := make(readGate, 1)
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := gate.run(context.Background(), func() (string, error) {
				current := active.Add(1)
				for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				return "complete result", nil
			})
			if err != nil || value != "complete result" {
				t.Errorf("read result %q, %v", value, err)
			}
		}()
	}
	wg.Wait()
	if got := peak.Load(); got != 1 {
		t.Fatalf("concurrent APT reads: %d", got)
	}
}

func TestAptReadGateCancelsQueuedReadsAndReleasesOnFailure(t *testing.T) {
	gate := make(readGate, 1)
	gate <- struct{}{} // another subprocess owns the slot
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := gate.run(ctx, func() (string, error) {
			t.Error("cancelled queued read started a subprocess")
			return "", nil
		})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation blocked behind another read")
	}
	<-gate
	wantErr := errors.New("query failed")
	_, err := gate.run(context.Background(), func() (string, error) { return "", wantErr })
	if !errors.Is(err, wantErr) || len(gate) != 0 {
		t.Fatalf("failed read leaked its slot: %v", err)
	}
	_, err = gate.run(context.Background(), func() (string, error) { return "next complete result", nil })
	if err != nil {
		t.Fatal(err)
	}
}
