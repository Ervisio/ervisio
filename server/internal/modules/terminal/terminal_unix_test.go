//go:build !windows

package terminal

import (
	"strings"
	"testing"
)

// /etc/passwd login shells exist on Unix only.

func TestParsePasswdShell(t *testing.T) {
	pw := "root:x:0:0:root:/root:/bin/sh\nbob:x:1000:1000::/home/bob:/usr/bin/nologin\nann:x:1001:1001::/home/ann:\n"
	if got := parsePasswdShell(strings.NewReader(pw), "root"); got != "/bin/sh" {
		t.Fatalf("root: %q", got)
	}
	for _, u := range []string{"bob", "ann", "nobody"} {
		if got := parsePasswdShell(strings.NewReader(pw), u); got != "" {
			t.Fatalf("%s: %q", u, got)
		}
	}
}
