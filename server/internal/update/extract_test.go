package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	body     string
	typ      byte
	mode     int64
	linkname string
	size     int64 // override header size (0 = len(body))
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Linkname: e.linkname, Uid: 1000, Gid: 1000}
		if typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if typ == tar.TypeXGlobalHeader {
			h = &tar.Header{Typeflag: typ, PAXRecords: map[string]string{"comment": "git"}}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

const pfx = "linuxadmin-1.2.0-linux-amd64"

func TestExtractGood(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, arc, []tarEntry{
		{name: pfx + "/", typ: tar.TypeDir, mode: 0o755},
		{name: pfx + "/bin/", typ: tar.TypeDir, mode: 0o777},
		{name: pfx + "/bin/linuxadmind", body: "#!bin", mode: 0o4755}, // setuid dropped
		{name: "./" + pfx + "/web/assets/app.js", body: "js", mode: 0o666},
		{name: pfx + "/VERSION", body: "1.2.0\n"},
		{typ: tar.TypeXGlobalHeader, name: "pax_global_header"},
	})
	dst := filepath.Join(dir, "out")
	if err := ExtractTarGz(arc, dst, pfx, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	check := func(rel string, mode os.FileMode, body string) {
		fi, err := os.Lstat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode() != mode {
			t.Errorf("%s mode %v, want %v", rel, fi.Mode(), mode)
		}
		if body != "" {
			b, _ := os.ReadFile(filepath.Join(dst, rel))
			if string(b) != body {
				t.Errorf("%s = %q", rel, b)
			}
		}
	}
	check("bin/linuxadmind", 0o755, "#!bin")
	check("web/assets/app.js", 0o644, "js")
	check("VERSION", 0o644, "1.2.0\n")
	check("bin", os.ModeDir|0o755, "")
}

func TestExtractRejects(t *testing.T) {
	cases := map[string][]tarEntry{
		"traversal":        {{name: pfx + "/../evil", body: "x"}},
		"traversal-deep":   {{name: pfx + "/bin/../../evil", body: "x"}},
		"absolute":         {{name: "/etc/passwd", body: "x"}},
		"outside-prefix":   {{name: "other/bin/x", body: "x"}},
		"prefix-lookalike": {{name: pfx + "-evil/x", body: "x"}},
		"symlink":          {{name: pfx + "/bin/sh", typ: tar.TypeSymlink, linkname: "/bin/sh"}},
		"hardlink":         {{name: pfx + "/passwd", typ: tar.TypeLink, linkname: "/etc/passwd"}},
		"chardev":          {{name: pfx + "/dev", typ: tar.TypeChar}},
		"fifo":             {{name: pfx + "/fifo", typ: tar.TypeFifo}},
		"duplicate":        {{name: pfx + "/a", body: "1"}, {name: pfx + "/a", body: "2"}},
		"dot-component":    {{name: pfx + "/./a", body: "x"}},
		"backslash":        {{name: pfx + "/a\\b", body: "x"}},
		"control-char":     {{name: pfx + "/a\nb", body: "x"}},
		"file-then-dir":    {{name: pfx + "/a", body: "x"}, {name: pfx + "/a/b", body: "y"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, "a.tar.gz")
			writeTarGz(t, arc, entries)
			dst := filepath.Join(dir, "out")
			if err := ExtractTarGz(arc, dst, pfx, DefaultLimits); err == nil {
				t.Fatal("accepted")
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatal("destination left behind after a failed extraction")
			}
			if _, err := os.Stat(filepath.Join(dir, "evil")); !os.IsNotExist(err) {
				t.Fatal("file written outside the destination")
			}
		})
	}
}

func TestExtractLimits(t *testing.T) {
	dir := t.TempDir()
	arc := filepath.Join(dir, "a.tar.gz")
	writeTarGz(t, arc, []tarEntry{{name: pfx + "/big", body: strings.Repeat("x", 2000)}})
	if err := ExtractTarGz(arc, filepath.Join(dir, "o1"), pfx, Limits{MaxEntries: 10, MaxFile: 1000, MaxTotal: 1 << 20}); err == nil {
		t.Fatal("file over MaxFile accepted")
	}
	writeTarGz(t, arc, []tarEntry{{name: pfx + "/a", body: strings.Repeat("x", 600)}, {name: pfx + "/b", body: strings.Repeat("x", 600)}})
	if err := ExtractTarGz(arc, filepath.Join(dir, "o2"), pfx, Limits{MaxEntries: 10, MaxFile: 1000, MaxTotal: 1000}); err == nil {
		t.Fatal("archive over MaxTotal accepted")
	}
	var many []tarEntry
	for i := 0; i < 20; i++ {
		many = append(many, tarEntry{name: pfx + "/f" + string(rune('a'+i)), body: "x"})
	}
	writeTarGz(t, arc, many)
	if err := ExtractTarGz(arc, filepath.Join(dir, "o3"), pfx, Limits{MaxEntries: 10, MaxFile: 1000, MaxTotal: 1 << 20}); err == nil {
		t.Fatal("archive over MaxEntries accepted")
	}
	// Not a gzip at all.
	os.WriteFile(arc, []byte("plain text"), 0o644)
	if err := ExtractTarGz(arc, filepath.Join(dir, "o4"), pfx, DefaultLimits); err == nil {
		t.Fatal("non-gzip accepted")
	}
	// Existing destination is refused (never extract over something).
	os.Mkdir(filepath.Join(dir, "o5"), 0o755)
	writeTarGz(t, arc, []tarEntry{{name: pfx + "/a", body: "x"}})
	if err := ExtractTarGz(arc, filepath.Join(dir, "o5"), pfx, DefaultLimits); err == nil {
		t.Fatal("existing destination accepted")
	}
}
