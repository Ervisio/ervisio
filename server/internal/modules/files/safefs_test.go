package files

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// These tests play the attacker of docs/SECURITY-REVIEW.md H1: a user who owns
// part of a tree that the root bridge works on and swaps directories for links
// while the walk runs. They run unprivileged, so "outside" is just a folder the
// operation must never touch.

func setHook(t *testing.T, f func(stage, full string)) {
	t.Helper()
	old := raceHook
	raceHook = f
	t.Cleanup(func() { raceHook = old })
}

// strict turns on the root bridge's link policy: only links owned by
// trustedLinkUID are followed, and links made by the test belong to the test
// user, i.e. "another user".
func strict(t *testing.T) {
	t.Helper()
	oldS, oldU := strictLinks, trustedLinkUID
	strictLinks, trustedLinkUID = true, 0
	if os.Geteuid() == 0 {
		trustedLinkUID = 1 << 31 // as root the test's own links must not count as trusted either
	}
	t.Cleanup(func() { strictLinks, trustedLinkUID = oldS, oldU })
}

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// swapForLink replaces dir with a link to target, keeping the old directory
// at dir+".old" (as an attacker with rename rights would).
func swapForLink(t *testing.T, dir, target string) {
	t.Helper()
	if err := os.Rename(dir, dir+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
}

// layout: d/tree/a/f (0644), d/outside/f (0644) and d/outside/a/f.
func raceLayout(t *testing.T) (tree, outside string) {
	d := t.TempDir()
	tree, outside = d+"/tree", d+"/outside"
	write(t, tree+"/a/f", "mine")
	write(t, outside+"/f", "secret")
	write(t, outside+"/a/f", "secret")
	for _, p := range []string{tree + "/a/f", outside + "/f", outside + "/a/f"} {
		os.Chmod(p, 0o644)
	}
	return
}

func chmodCall(t *testing.T, path, mode string, rec bool) (any, error) {
	raw, _ := json.Marshal(map[string]any{"path": path, "mode": mode, "recursive": rec})
	return hChmod(context.Background(), &rpc.Call{Params: raw})
}

func TestChmodRecursiveSwapDoesNotEscape(t *testing.T) {
	for _, stage := range []string{"lstat", "open", "children"} {
		t.Run(stage, func(t *testing.T) {
			tree, outside := raceLayout(t)
			swapped := false
			setHook(t, func(st, full string) {
				if st == stage && full == tree+"/a" && !swapped {
					swapped = true
					swapForLink(t, tree+"/a", outside)
				}
			})
			if _, err := chmodCall(t, tree, "0700", true); err != nil {
				t.Fatal(err)
			}
			if !swapped {
				t.Fatal("hook never ran")
			}
			for _, p := range []string{outside, outside + "/f", outside + "/a", outside + "/a/f"} {
				if m := modeOf(t, p); m == 0o700 {
					t.Errorf("%s was changed through the swapped link", p)
				}
			}
			if stage == "children" {
				// The walk keeps working on the directory it opened (now a.old).
				if m := modeOf(t, tree+"/a.old/f"); m != 0o700 {
					t.Errorf("opened directory not walked: %v", m)
				}
			}
		})
	}
}

func TestChownRecursiveSwapDoesNotEscape(t *testing.T) {
	gid := -1
	groups, _ := os.Getgroups()
	for _, g := range groups {
		if g != os.Getgid() {
			gid = g
			break
		}
	}
	if gid < 0 {
		t.Skip("needs a supplementary group")
	}
	gidOf := func(p string) int {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		return int(fi.Sys().(*syscall.Stat_t).Gid)
	}
	for _, stage := range []string{"lstat", "open", "children"} {
		t.Run(stage, func(t *testing.T) {
			tree, outside := raceLayout(t)
			swapped := false
			setHook(t, func(st, full string) {
				if st == stage && full == tree+"/a" && !swapped {
					swapped = true
					swapForLink(t, tree+"/a", outside)
				}
			})
			raw, _ := json.Marshal(map[string]any{"path": tree, "group": strconv.Itoa(gid), "recursive": true})
			if _, err := hChown(context.Background(), &rpc.Call{Params: raw}); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{outside, outside + "/f", outside + "/a", outside + "/a/f"} {
				if gidOf(p) == gid {
					t.Errorf("%s was changed through the swapped link", p)
				}
			}
			if gidOf(tree) != gid {
				t.Error("tree itself not changed")
			}
		})
	}
}

func TestCopySwapDoesNotLeak(t *testing.T) {
	cases := []struct{ name, stage, swap string }{
		{"dir-before-open", "open", "/a"},
		{"file-before-open", "open", "/a/f"},
		{"dir-after-open", "children", "/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, outside := raceLayout(t)
			dst := filepath.Dir(tree) + "/dst"
			os.Mkdir(dst, 0o755)
			swapped := false
			setHook(t, func(st, full string) {
				if st == tc.stage && full == tree+tc.swap && !swapped {
					swapped = true
					target := outside
					if strings.HasSuffix(tc.swap, "/f") {
						target = outside + "/f"
					}
					swapForLink(t, tree+tc.swap, target)
				}
			})
			_ = doCopy(context.Background(), copyParams{From: []string{tree}, To: dst}, newStream())
			if !swapped {
				t.Fatal("hook never ran")
			}
			_ = filepath.Walk(dst, func(p string, fi os.FileInfo, err error) error {
				if err == nil && fi.Mode().IsRegular() {
					if b, _ := os.ReadFile(p); string(b) == "secret" {
						t.Errorf("%s holds the outside file's content", p)
					}
				}
				return nil
			})
		})
	}
}

func TestStrictRefusesUserLinksInPaths(t *testing.T) {
	strict(t)
	d := t.TempDir()
	write(t, d+"/outside/f", "secret")
	os.Chmod(d+"/outside/f", 0o644)
	os.Mkdir(d+"/home", 0o755)
	if err := os.Symlink(d+"/outside", d+"/home/link"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(d+"/outside/f", d+"/home/flink"); err != nil {
		t.Fatal(err)
	}
	via := d + "/home/link/f"
	forbidden := func(what string, err error) {
		t.Helper()
		if !rpc.IsCode(err, rpc.Forbidden) {
			t.Errorf("%s through a user's link: %v", what, err)
		}
	}
	_, err := doDelete([]string{via}, false)
	forbidden("delete", err)
	_, err = chmodCall(t, via, "0600", false)
	forbidden("chmod", err)
	raw, _ := json.Marshal(map[string]any{"path": via, "group": strconv.Itoa(os.Getgid())})
	_, err = hChown(context.Background(), &rpc.Call{Params: raw})
	forbidden("chown", err)
	_, err = writeTextFile(via, []byte("pwned"), 0)
	forbidden("writeText via folder link", err)
	_, err = writeTextFile(d+"/home/flink", []byte("pwned"), 0)
	forbidden("writeText via file link", err)
	forbidden("move from", movePath(via, d+"/home/moved"))
	forbidden("move to", movePath(d+"/home/flink", d+"/home/link/new"))
	forbidden("mkdir -p", mkdirAll(d+"/home/link/new/deeper", 0o755))
	forbidden("copy", doCopy(context.Background(), copyParams{From: []string{via}, To: d + "/home"}, newStream()))
	forbidden("copy into", doCopy(context.Background(), copyParams{From: []string{d + "/home/flink"}, To: d + "/home/link"}, newStream()))
	raw, _ = json.Marshal(map[string]any{"path": via, "size": 1, "overwrite": true})
	forbidden("upload", hWriteStream(context.Background(), &rpc.Call{Params: raw}, newStream()))

	if b, _ := os.ReadFile(d + "/outside/f"); string(b) != "secret" {
		t.Errorf("outside file changed: %q", b)
	}
	if modeOf(t, d+"/outside/f") != 0o644 {
		t.Error("outside file mode changed")
	}
	if _, err := os.Stat(d + "/outside/new"); err == nil {
		t.Error("folder created through the link")
	}

	// The link itself can still be deleted (that only touches the link).
	if _, err := doDelete([]string{d + "/home/flink"}, false); err != nil {
		t.Errorf("deleting the link itself: %v", err)
	}
	if _, err := os.Stat(d + "/outside/f"); err != nil {
		t.Error("deleting the link removed its target")
	}

	// A link owned by the trusted user (root in production) is followed.
	trustedLinkUID = uint32(os.Getuid())
	if _, err := writeTextFile(via, []byte("ok"), 0); err != nil {
		t.Errorf("trusted link refused: %v", err)
	}
}

func TestChmodNoFollowRefusesLink(t *testing.T) {
	d := t.TempDir()
	write(t, d+"/outside/f", "secret")
	os.Chmod(d+"/outside/f", 0o644)
	os.Mkdir(d+"/home", 0o755)
	os.Symlink(d+"/outside/f", d+"/home/notes")
	f, name, err := openParent(d + "/home/notes")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := chmodNoFollow(dfd(f), name, 0o600); err != errIsLink {
		t.Errorf("chmod must refuse a link, got %v", err)
	}
	if modeOf(t, d+"/outside/f") != 0o644 {
		t.Error("link target changed")
	}
	// Saving over a file that became a link: the in-place fallback opens with
	// O_NOFOLLOW, the normal path renames over the link itself.
	strict(t)
	if _, err := writeTextFile(d+"/home/notes", []byte("x"), 0); !rpc.IsCode(err, rpc.Forbidden) {
		t.Errorf("save through a user's file link: %v", err)
	}
}

func TestMkdirAllAndCreateStayPut(t *testing.T) {
	d := t.TempDir()
	if err := mkdirAll(d+"/a/b/c", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mkdirAll(d+"/a/b/c", 0o755); err != nil {
		t.Fatal("existing tree:", err)
	}
	raw, _ := json.Marshal(map[string]any{"path": d + "/a/b/c/new.txt"})
	if _, err := hCreate(context.Background(), &rpc.Call{Params: raw}); err != nil {
		t.Fatal(err)
	}
	if _, err := hCreate(context.Background(), &rpc.Call{Params: raw}); !os.IsExist(err) {
		t.Errorf("create over an existing file: %v", err)
	}
	// A dangling link in place of the new name is never followed by create.
	os.Symlink(d+"/elsewhere", d+"/a/dangling")
	raw, _ = json.Marshal(map[string]any{"path": d + "/a/dangling"})
	if _, err := hCreate(context.Background(), &rpc.Call{Params: raw}); err == nil {
		t.Error("create followed a dangling link")
	}
	if _, err := os.Lstat(d + "/elsewhere"); err == nil {
		t.Error("file created at the link target")
	}
}

func TestMoveAcrossDevicesFallback(t *testing.T) {
	// Copy+remove path of movePath, exercised directly through the copier.
	d := t.TempDir()
	write(t, d+"/src/x/y", "data")
	os.Symlink("y", d+"/src/x/ln")
	if err := copyPath(context.Background(), d+"/src", d+"/dst", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d + "/dst/x/y"); string(b) != "data" {
		t.Error("content")
	}
	if l, _ := os.Readlink(d + "/dst/x/ln"); l != "y" {
		t.Error("link not copied as a link")
	}
	// Copying a folder into itself through another name stops at the copy.
	if err := copyPath(context.Background(), d+"/src", d+"/src/x/inner", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d + "/src/x/inner/x/inner"); err == nil {
		t.Error("copy recursed into itself")
	}
	// A failed copy never removes a target it did not create.
	write(t, d+"/taken", "keep")
	if err := copyPath(context.Background(), d+"/src", d+"/taken", nil); err == nil {
		t.Error("copy over an existing file")
	}
	if b, _ := os.ReadFile(d + "/taken"); string(b) != "keep" {
		t.Error("existing target removed")
	}
}
