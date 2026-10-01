package server

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/account"
	"github.com/Fonlogen/LinuxAdmin/server/internal/bridge"
)

// A root bridge with a call or stream in progress is never stopped for
// idleness; once released, the normal admin idle timeout applies again.
func TestRootBridgeHeldWhileBusy(t *testing.T) {
	a, err := account.Current()
	if err != nil {
		t.Skip(err)
	}
	spec := &bridge.Spec{Bridge: buildBridge(t), Config: filepath.Join(t.TempDir(), "c.conf"), Account: a, Logger: log.New(io.Discard, "", 0)}
	p, err := bridge.StartUser(context.Background(), spec) // stands in for the root bridge
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	st := newStore()
	sess := &Session{Account: a, Created: time.Now(), lastSeen: time.Now()}
	if _, err := st.add(sess); err != nil {
		t.Fatal(err)
	}
	sess.mu.Lock()
	sess.root = p
	sess.rootUsed = time.Now().Add(-time.Hour) // long idle
	sess.mu.Unlock()

	release := sess.hold(p, true)
	st.expire(time.Now(), 12*time.Hour, 5*time.Minute)
	time.Sleep(50 * time.Millisecond) // expire locks asynchronously
	sess.mu.Lock()
	held := sess.root == p
	sess.mu.Unlock()
	if !held {
		t.Fatal("busy root bridge was stopped")
	}
	if until := sess.unlockedUntil(5 * time.Minute); time.Until(until) < 4*time.Minute {
		t.Fatalf("busy unlock should report a full window, got %v", time.Until(until))
	}

	release() // also counts as admin activity
	st.expire(time.Now(), 12*time.Hour, 5*time.Minute)
	time.Sleep(50 * time.Millisecond)
	sess.mu.Lock()
	still := sess.root == p
	sess.rootUsed = time.Now().Add(-time.Hour)
	sess.mu.Unlock()
	if !still {
		t.Fatal("release must count as activity")
	}
	st.expire(time.Now(), 12*time.Hour, 5*time.Minute)
	deadline := time.Now().Add(5 * time.Second)
	for {
		sess.mu.Lock()
		gone := sess.root == nil
		sess.mu.Unlock()
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("idle root bridge not locked")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
