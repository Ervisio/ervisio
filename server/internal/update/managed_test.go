package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedBy(t *testing.T) {
	l := testLayout(t)
	if got := l.ManagedBy(); got != "" {
		t.Fatalf("no marker: %q", got)
	}
	l.Managed = filepath.Join(l.LibDir, "managed")
	if got := l.ManagedBy(); got != "" {
		t.Fatalf("marker missing: %q", got)
	}
	for content, want := range map[string]string{
		"pacman\n":              "pacman",
		"  apt  ":               "apt",
		"zypper":                "zypper",
		"":                      "unknown",
		"rm -rf /":              "unknown",
		"Pacman":                "unknown",
		strings.Repeat("a", 40): "unknown",
	} {
		os.WriteFile(l.Managed, []byte(content), 0o644)
		if got := l.ManagedBy(); got != want {
			t.Errorf("marker %q: got %q, want %q", content, got, want)
		}
	}
}
