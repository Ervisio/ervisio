package terminal

import (
	"github.com/ervisio/ervisio/server/internal/sys"
	"os"
	"strings"
	"testing"
)

func TestManagedPathKeepsSingleCaseInsensitiveVariable(t *testing.T) {
	before := sys.SafePath
	env := []string{"Path=/system/bin", "PATH=/duplicate", "HOME=/home/O'Brien; $(bad)", "TERM=xterm-256color"}
	dirs := []string{"/home/O'Brien; $(bad)/node/bin", "/home/account/codex/bin"}
	out := prependManagedPath(env, dirs)
	seen := 0
	for _, entry := range out {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "PATH") {
			seen++
			want := strings.Join(dirs, string(os.PathListSeparator)) + string(os.PathListSeparator) + "/system/bin"
			if value != want {
				t.Fatalf("PATH changed literals: %q", value)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("PATH duplicates: %v", out)
	}
	if !strings.Contains(strings.Join(out, "\n"), "HOME=/home/O'Brien; $(bad)") {
		t.Fatal("home variable changed")
	}
	if sys.SafePath != before {
		t.Fatal("global bridge PATH was mutated")
	}
	if env[0] != "Path=/system/bin" {
		t.Fatal("source environment was mutated")
	}
}
