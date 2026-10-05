//go:build unix

package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

type fakeStream struct {
	events []json.RawMessage
	bytes  [][]byte
	in     chan json.RawMessage
}

func newStream() *fakeStream { return &fakeStream{in: make(chan json.RawMessage, 64)} }
func (f *fakeStream) Send(v any) error {
	b, _ := json.Marshal(v)
	f.events = append(f.events, b)
	return nil
}
func (f *fakeStream) SendBytes(b []byte) error {
	f.bytes = append(f.bytes, append([]byte(nil), b...))
	return nil
}
func (f *fakeStream) Input() <-chan json.RawMessage { return f.in }

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCleanPath(t *testing.T) {
	for _, bad := range []string{"", "rel/x", "../x", "/a\x00b"} {
		if _, err := cleanPath(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if p, err := cleanPath("/a/b/../c//d/"); err != nil || p != "/a/c/d" {
		t.Errorf("got %q %v", p, err)
	}
}

func TestModeStrings(t *testing.T) {
	if s := modeString(0o755 | os.ModeSetuid); s != "rwsr-xr-x" {
		t.Error(s)
	}
	if s := modeString(0o644); s != "rw-r--r--" {
		t.Error(s)
	}
	if s := modeOctal(0o1777&0o777 | os.ModeSticky); s != "1777" {
		t.Error(s)
	}
	if m, err := parseMode("4755"); err != nil || m&os.ModeSetuid == 0 || m.Perm() != 0o755 {
		t.Error(m, err)
	}
	if _, err := parseMode("9"); err == nil {
		t.Error("9 accepted")
	}
}

func TestList(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/b.txt", "hi")
	write(t, d+"/.hidden", "x")
	os.Mkdir(d+"/Adir", 0o755)
	os.Symlink(d+"/Adir", d+"/link")
	es, _, err := listDir(d, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 || es[0].Name != "Adir" || es[1].Name != "link" || es[1].TargetType != "dir" || es[2].Name != "b.txt" {
		t.Fatalf("%+v", es)
	}
	if es[2].Mime != "text/plain" || es[2].Perm != "rw-r--r--" || es[2].Mode != "0644" || es[2].Owner == "" {
		t.Errorf("%+v", es[2])
	}
	all, _, _ := listDir(d, true)
	if len(all) != 4 {
		t.Error(len(all))
	}
	if _, _, err := listDir(d+"/b.txt", false); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
	if _, _, err := listDir(d+"/nope", false); !os.IsNotExist(err) {
		t.Error(err)
	}
}

func TestMoveNoOverwrite(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/a", "1")
	write(t, d+"/b", "2")
	if err := movePath(d+"/a", d+"/b"); !rpc.IsCode(err, rpc.Conflict) {
		t.Error(err)
	}
	os.Mkdir(d+"/dir", 0o755)
	if err := movePath(d+"/dir", d+"/dir/sub"); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
	if err := movePath(d+"/a", d+"/c"); err != nil {
		t.Error(err)
	}
}

func TestCopyStream(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/src/a.txt", "hello")
	write(t, d+"/src/sub/b.txt", "world!")
	os.Symlink("a.txt", d+"/src/ln")
	os.Mkdir(d+"/dst", 0o755)
	s := newStream()
	if err := doCopy(context.Background(), copyParams{From: []string{d + "/src"}, To: d + "/dst"}, s); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d + "/dst/src/sub/b.txt"); string(b) != "world!" {
		t.Error("content")
	}
	if l, _ := os.Readlink(d + "/dst/src/ln"); l != "a.txt" {
		t.Error("symlink", l)
	}
	last := string(s.events[len(s.events)-1])
	if !strings.Contains(last, `"done":true`) || !strings.Contains(string(s.events[0]), `"total":11`) {
		t.Error(last, string(s.events[0]))
	}
	// copy again: gets a new name
	if err := doCopy(context.Background(), copyParams{From: []string{d + "/src"}, To: d + "/dst"}, newStream()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d + "/dst/src (copy)/a.txt"); err != nil {
		t.Error(err)
	}
	// into itself
	if err := doCopy(context.Background(), copyParams{From: []string{d + "/src"}, To: d + "/src/sub"}, newStream()); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
	// move
	if err := doCopy(context.Background(), copyParams{From: []string{d + "/dst/src"}, To: d + "/src", Move: true}, newStream()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d + "/dst/src"); !os.IsNotExist(err) {
		t.Error("source still there")
	}
}

func TestUniqueName(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/a.tar.gz", "")
	write(t, d+"/.rc", "")
	if g := uniqueName(d + "/a.tar.gz"); filepath.Base(g) != "a.tar (copy).gz" {
		t.Error(g)
	}
	if g := uniqueName(d + "/.rc"); filepath.Base(g) != ".rc (copy)" {
		t.Error(g)
	}
}

func TestDeleteDoesNotFollowSymlinks(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/outside/keep.txt", "keep")
	write(t, d+"/victim/x", "x")
	os.Symlink(d+"/outside", d+"/victim/escape")
	res, err := doDelete([]string{d + "/victim"}, false)
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	if _, err := os.Stat(d + "/victim"); !os.IsNotExist(err) {
		t.Error("victim remains")
	}
	if _, err := os.Stat(d + "/outside/keep.txt"); err != nil {
		t.Error("followed the symlink out:", err)
	}
	if _, err := doDelete([]string{"/"}, false); !rpc.IsCode(err, rpc.Forbidden) {
		t.Error(err)
	}
	if _, err := doDelete([]string{d + "/missing"}, false); !os.IsNotExist(err) {
		t.Error(err)
	}
}

func TestTrashRoundTrip(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	homeOverride = h
	defer func() { homeOverride = "" }()
	d := t.TempDir()
	write(t, d+"/my file #1.txt", "data")
	if _, err := doDelete([]string{d + "/my file #1.txt"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d + "/my file #1.txt"); !os.IsNotExist(err) {
		t.Fatal("still there")
	}
	info, _ := os.ReadFile(h + "/.local/share/Trash/info/my file #1.txt.trashinfo")
	if !strings.Contains(string(info), "[Trash Info]\nPath="+strings.ReplaceAll(strings.ReplaceAll(d, " ", "%20"), "#", "%23")) || !strings.Contains(string(info), "%20%231.txt") {
		t.Errorf("info: %s", info)
	}
	// same name again
	write(t, d+"/my file #1.txt", "data2")
	doDelete([]string{d + "/my file #1.txt"}, true)
	r, err := hTrashList(context.Background(), &rpc.Call{})
	if err != nil {
		t.Fatal(err)
	}
	items := r.(map[string]any)["items"].([]trashItem)
	if len(items) != 2 || items[0].OriginalPath != d+"/my file #1.txt" || items[0].Entry.Name != "my file #1.txt" {
		t.Fatalf("%+v", items)
	}
	raw, _ := json.Marshal(map[string]any{"ids": []string{items[0].ID, items[1].ID}})
	res, err := hRestore(context.Background(), &rpc.Call{Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if len(m["restored"].([]string)) != 1 || len(m["failed"].([]failure)) != 1 { // second conflicts
		t.Fatalf("%+v", m)
	}
	if _, err := hTrashEmpty(context.Background(), &rpc.Call{}); err != nil {
		t.Fatal(err)
	}
	r, _ = hTrashList(context.Background(), &rpc.Call{})
	if len(r.(map[string]any)["items"].([]trashItem)) != 0 {
		t.Error("not empty")
	}
}

func TestChmodRecursive(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/t/a", "")
	os.Symlink("/etc/passwd", d+"/t/l")
	call := func(m map[string]any) (any, error) {
		raw, _ := json.Marshal(m)
		return hChmod(context.Background(), &rpc.Call{Params: raw})
	}
	if _, err := call(map[string]any{"path": d + "/t", "mode": "700", "recursive": true}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(d + "/t/a"); fi.Mode().Perm() != 0o700 {
		t.Error(fi.Mode())
	}
	if _, err := call(map[string]any{"path": d + "/t/l", "mode": "700"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
	if _, err := call(map[string]any{"path": d + "/t", "mode": "abc"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
}

func TestText(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/a.txt", "héllo\nworld\n")
	call := func(m string, p map[string]any) (any, error) {
		raw, _ := json.Marshal(p)
		c := &rpc.Call{Params: raw}
		if m == "r" {
			return hReadText(context.Background(), c)
		}
		return hWriteText(context.Background(), c)
	}
	r, err := call("r", map[string]any{"path": d + "/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	m := r.(map[string]any)
	if m["encoding"] != "utf-8" || m["content"] != "héllo\nworld\n" {
		t.Fatalf("%v", m)
	}
	mt := m["mtime"].(int64)
	// truncation does not cut a rune
	r, _ = call("r", map[string]any{"path": d + "/a.txt", "maxBytes": 2})
	if r.(map[string]any)["content"] != "h" || r.(map[string]any)["truncated"] != true {
		t.Errorf("%v", r)
	}
	// write with the right mtime
	if _, err := call("w", map[string]any{"path": d + "/a.txt", "content": "new", "expectedMtime": mt}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d + "/a.txt"); string(b) != "new" {
		t.Error(string(b))
	}
	// stale mtime
	if _, err := call("w", map[string]any{"path": d + "/a.txt", "content": "x", "expectedMtime": mt - 5000}); !rpc.IsCode(err, rpc.Conflict) {
		t.Error(err)
	}
	// binary and latin1
	os.WriteFile(d+"/bin", []byte{1, 2, 0, 3}, 0o644)
	if _, err := call("r", map[string]any{"path": d + "/bin"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
	os.WriteFile(d+"/lat", []byte{'c', 'a', 'f', 0xE9}, 0o644)
	r, _ = call("r", map[string]any{"path": d + "/lat"})
	if r.(map[string]any)["encoding"] != "latin1" || r.(map[string]any)["content"] != "café" {
		t.Errorf("%v", r)
	}
	if _, err := call("w", map[string]any{"path": d + "/lat", "content": "café", "encoding": "latin1"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d + "/lat"); !bytes.Equal(b, []byte{'c', 'a', 'f', 0xE9}) {
		t.Error(b)
	}
	os.Mkdir(d+"/dir", 0o755)
	if _, err := call("r", map[string]any{"path": d + "/dir"}); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
}

func TestSearch(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/a/Report.pdf", "")
	write(t, d+"/b/c/report-2.txt", "")
	write(t, d+"/b/other", "")
	s := newStream()
	if err := doSearch(context.Background(), searchParams{Root: d, Query: "REPORT"}, s); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, e := range s.events {
		var m struct {
			Entries []Entry
			Done    bool
			Count   int
		}
		json.Unmarshal(e, &m)
		n += len(m.Entries)
		if m.Done && m.Count != 2 {
			t.Error(string(e))
		}
	}
	if n != 2 {
		t.Error(n)
	}
	s = newStream()
	doSearch(context.Background(), searchParams{Root: d, Query: "*.txt", MaxResults: 1}, s)
	if !strings.Contains(string(s.events[len(s.events)-1]), `"truncated":true`) {
		t.Error(string(s.events[len(s.events)-1]))
	}
}

func TestThumbnail(t *testing.T) {
	d := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{200, 10, 10, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.WriteFile(d+"/i.png", buf.Bytes(), 0o644)
	raw, _ := json.Marshal(map[string]any{"path": d + "/i.png", "size": 100})
	r, err := hThumbnail(context.Background(), &rpc.Call{Params: raw})
	if err != nil {
		t.Fatal(err)
	}
	m := r.(map[string]any)
	if m["width"] != 100 || m["height"] != 50 {
		t.Errorf("%v %v", m["width"], m["height"])
	}
	b, _ := base64.StdEncoding.DecodeString(m["data"].(string))
	out, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.RGBAModel.Convert(out.At(10, 10)).(color.RGBA); c.R < 190 || c.G > 20 {
		t.Error(c)
	}
	os.WriteFile(d+"/x.png", []byte("nope"), 0o644)
	raw, _ = json.Marshal(map[string]any{"path": d + "/x.png"})
	if _, err := hThumbnail(context.Background(), &rpc.Call{Params: raw}); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
}

func TestMountinfo(t *testing.T) {
	sample := `22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
23 22 0:20 / /proc rw - proc proc rw
30 22 259:2 /home /home rw - ext4 /dev/nvme0n1p2 rw
31 22 259:1 / /boot rw - vfat /dev/nvme0n1p1 rw
32 22 7:0 / /snap/x ro - squashfs /dev/loop0 ro
33 22 8:1 / /mnt/My\040Disk rw - exfat /dev/sda1 rw
34 22 0:50 / /tmp rw - tmpfs tmpfs rw`
	ds := pickDisks(parseMountinfo(sample))
	if len(ds) != 3 || ds[0].Mount != "/" || ds[2].Mount != "/mnt/My Disk" {
		t.Fatalf("%+v", ds)
	}
}

func TestRecent(t *testing.T) {
	x := `<?xml version="1.0"?><xbel version="1.0" xmlns:bookmark="http://www.freedesktop.org/standards/desktop-bookmarks">
<bookmark href="file:///home/u/old%20file.txt" added="2024-01-01T10:00:00Z" modified="2024-01-01T10:00:00Z" visited="2024-01-01T10:00:00Z"><info/></bookmark>
<bookmark href="file:///home/u/new.txt" added="2024-02-01T10:00:00Z" modified="2024-02-01T10:00:00Z" visited="2024-02-01T10:00:00Z"/>
<bookmark href="https://example.com/x" modified="2025-02-01T10:00:00Z"/></xbel>`
	r := parseRecent([]byte(x))
	if len(r) != 2 || r[0].Path != "/home/u/new.txt" || r[1].Path != "/home/u/old file.txt" {
		t.Fatalf("%+v", r)
	}
	_ = time.Now
}

func TestStreams(t *testing.T) {
	d := t.TempDir()
	payload := bytes.Repeat([]byte("abc"), 50000)
	// upload
	s := newStream()
	for i := 0; i < len(payload); i += 65536 {
		e := i + 65536
		if e > len(payload) {
			e = len(payload)
		}
		b, _ := json.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(payload[i:e])})
		s.in <- b
	}
	s.in <- json.RawMessage(`{"eof":true}`)
	raw, _ := json.Marshal(map[string]any{"path": d + "/up.bin", "size": len(payload)})
	if err := hWriteStream(context.Background(), &rpc.Call{Params: raw}, s); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d + "/up.bin"); !bytes.Equal(b, payload) {
		t.Fatal("upload mismatch")
	}
	// conflict
	if err := hWriteStream(context.Background(), &rpc.Call{Params: raw}, newStream()); !rpc.IsCode(err, rpc.Conflict) {
		t.Error(err)
	}
	// short upload is rejected and leaves no temp file
	s = newStream()
	s.in <- json.RawMessage(`{"data":"YWJj"}`)
	s.in <- json.RawMessage(`{"eof":true}`)
	raw, _ = json.Marshal(map[string]any{"path": d + "/short.bin", "size": 10})
	if err := hWriteStream(context.Background(), &rpc.Call{Params: raw}, s); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
	des, _ := os.ReadDir(d)
	if len(des) != 1 {
		t.Errorf("leftovers: %v", des)
	}
	// download
	rs := newStream()
	raw, _ = json.Marshal(map[string]any{"path": d + "/up.bin"})
	if err := hReadStream(context.Background(), &rpc.Call{Params: raw}, rs); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for _, b := range rs.bytes {
		got = append(got, b...)
	}
	if !bytes.Equal(got, payload) || !strings.Contains(string(rs.events[0]), `"size":150000`) || !strings.Contains(string(rs.events[len(rs.events)-1]), "done") {
		t.Error("download mismatch")
	}
	raw, _ = json.Marshal(map[string]any{"path": d})
	if err := hReadStream(context.Background(), &rpc.Call{Params: raw}, newStream()); !rpc.IsCode(err, rpc.Invalid) {
		t.Error(err)
	}
}
