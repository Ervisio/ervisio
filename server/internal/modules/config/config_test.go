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
