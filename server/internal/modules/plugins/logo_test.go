package plugins

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindLogo(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{}
	if got := findLogo(dir, m); got != "" {
		t.Fatalf("no logo file: got %q", got)
	}
	os.WriteFile(filepath.Join(dir, "logo.png"), []byte("png"), 0o644)
	if got := findLogo(dir, m); got != "logo.png" {
		t.Fatalf("png only: got %q", got)
	}
	os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("<svg/>"), 0o644)
	if got := findLogo(dir, m); got != "logo.svg" {
		t.Fatalf("svg is preferred: got %q", got)
	}
	// A signed plugin shows only a logo its signature covers.
	signed := &Manifest{Files: map[string]string{"index.js": strings.Repeat("0", 64), "logo.png": strings.Repeat("0", 64)}}
	if got := findLogo(dir, signed); got != "logo.png" {
		t.Fatalf("signed: got %q", got)
	}
	// Too large, empty or a symlink: no logo.
	big := t.TempDir()
	os.WriteFile(filepath.Join(big, "logo.svg"), make([]byte, MaxLogoSize+1), 0o644)
	os.WriteFile(filepath.Join(big, "logo.png"), nil, 0o644)
	if got := findLogo(big, m); got != "" {
		t.Fatalf("oversized/empty: got %q", got)
	}
	link := t.TempDir()
	os.Symlink(filepath.Join(dir, "logo.svg"), filepath.Join(link, "logo.svg"))
	if got := findLogo(link, m); got != "" {
		t.Fatalf("symlink: got %q", got)
	}
}

func TestValidLogoURL(t *testing.T) {
	svg := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))
	for s, want := range map[string]bool{
		"":                                      true,
		svg:                                     true,
		"data:image/png;base64,iVBORw0KGgo=":    true,
		"https://example.com/logo.svg":          false,
		"data:text/html;base64,PHNjcmlwdD4=":    false,
		"data:image/svg+xml,<svg/>":             false,
		"data:image/svg+xml;base64,not base64!": false,
		"data:image/png;base64," + strings.Repeat("A", 100<<10): false,
	} {
		if got := ValidLogoURL(s); got != want {
			t.Errorf("ValidLogoURL(%.40q) = %v, want %v", s, got, want)
		}
	}
	c, err := parseCatalog([]byte(`{"plugins":[{"id":"aa","version":"1.0.0","logo":"javascript:alert(1)"},{"id":"bb","version":"1.0.0","logo":"` + svg + `"}]}`))
	if err != nil || c.Plugins[0].Logo != "" || c.Plugins[1].Logo != svg {
		t.Fatalf("parseCatalog logos: %v %+v", err, c)
	}
}
