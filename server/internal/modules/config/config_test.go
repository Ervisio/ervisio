package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

func TestConfigModule(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "linuxadmin.conf")
	st, err := get()
	if err != nil || st.Exists || st.Values["login.max_failures"] != 5 {
		t.Fatalf("%+v %v", st, err)
	}
	d, err := rawDiff(map[string]any{"login.show_ip": false})
	if err != nil || !strings.Contains(d.Diff, "-show_ip = true") || !strings.Contains(d.Diff, "+show_ip = false") {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := set("session.timeout", "2h"); err != nil {
		t.Fatal(err)
	}
	st, _ = get()
	if !st.Exists || st.Values["session.timeout"] != "2h" {
		t.Fatalf("%+v", st.Values)
	}
	if _, err := set("session.timeout", 5.0); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
	if _, err := set("bogus", true); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
}

func TestSetCrossKeyAndLists(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "linuxadmin.conf")
	// Plain HTTP on the default public listen address is refused as invalid.
	if _, err := set("tls.mode", "http"); !rpc.IsCode(err, rpc.Invalid) || !strings.Contains(err.Error(), "loopback") {
		t.Fatal(err)
	}
	if _, err := set("listen", "127.0.0.1:9090"); err != nil {
		t.Fatal(err)
	}
	if _, err := set("tls.mode", "http"); err != nil {
		t.Fatal(err)
	}
	st, err := set("auth.allow_users", []any{"alice", "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := st.Values["auth.allow_users"].([]string); len(l) != 2 {
		t.Fatalf("%v", st.Values["auth.allow_users"])
	}
	if _, err := set("auth.allow_groups", []any{"bad name"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Fatal(err)
	}
}
