package notify

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPluginRateLimitIsPerSender(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "n.json"))
	now := time.Unix(1000, 0)
	s.Now = func() time.Time { return now }
	for i := 0; i < 60; i++ {
		now = now.Add(7 * time.Second)
		if ok, _ := s.AllowPlugin("docker", "user:mallory"); !ok {
			t.Fatalf("blocked early at %d", i)
		}
	}
	now = now.Add(time.Minute)
	if ok, _ := s.AllowPlugin("docker", "user:mallory"); ok {
		t.Fatal("mallory is over the hourly limit")
	}
	if ok, _ := s.AllowPlugin("docker", "job:1234abcd"); !ok {
		t.Fatal("a job's notify step was blocked by a user's calls")
	}
	if ok, _ := s.AllowPlugin("docker", "user:alice"); !ok {
		t.Fatal("another user was blocked")
	}
}
