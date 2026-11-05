package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultShell(t *testing.T) {
	c := Default()
	if c.UI.DefaultShell != "classic" {
		t.Fatalf("existing installations must keep classic: %q", c.UI.DefaultShell)
	}
	for _, bad := range []any{"", "desktop", "ErvisiOS", true, 1.0} {
		if err := c.Set("ui.default_shell", bad); err == nil {
			t.Errorf("accepted default shell %#v", bad)
		}
	}
	if err := c.Set("ui.default_shell", "ervisios"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "ervisio.conf")
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, _, warnings, err := Load(p)
	if err != nil || len(warnings) != 0 || got.UI.DefaultShell != "ervisios" || got.Values()["ui.default_shell"] != "ervisios" {
		t.Fatalf("round trip: %+v %v %v", got, warnings, err)
	}
}
