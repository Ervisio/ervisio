package files

import (
	"context"
	"encoding/xml"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

type disk struct {
	Mount  string `json:"mount"`
	Device string `json:"device"`
	FS     string `json:"fstype"`
	Total  uint64 `json:"total"`
	Used   uint64 `json:"used"`
	Avail  uint64 `json:"avail"`
}

type mountRec struct{ Mount, FS, Source string }

func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, ok := octal3(s[i+1 : i+4]); ok {
				b.WriteByte(v)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func octal3(s string) (byte, bool) {
	if len(s) != 3 {
		return 0, false
	}
	v := 0
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, false
		}
		v = v*8 + int(c-'0')
	}
	return byte(v), true
}

// parseMountinfo reads /proc/self/mountinfo.
func parseMountinfo(data string) []mountRec {
	var out []mountRec
	for _, line := range strings.Split(data, "\n") {
		i := strings.Index(line, " - ")
		if i < 0 {
			continue
		}
		pre := strings.Fields(line[:i])
		post := strings.Fields(line[i+3:])
		if len(pre) < 5 || len(post) < 2 {
			continue
		}
		out = append(out, mountRec{Mount: unescapeMount(pre[4]), FS: post[0], Source: unescapeMount(post[1])})
	}
	return out
}

var networkFS = map[string]bool{"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "zfs": true, "fuse.sshfs": true}

func pickDisks(ms []mountRec) []mountRec {
	best := map[string]mountRec{}
	var order []string
	for _, m := range ms {
		real := strings.HasPrefix(m.Source, "/dev/") && !strings.HasPrefix(m.Source, "/dev/loop") && !strings.HasPrefix(m.Source, "/dev/ram")
		if !real && !networkFS[m.FS] {
			continue
		}
		key := m.Source
		if cur, ok := best[key]; ok {
			if len(m.Mount) < len(cur.Mount) {
				best[key] = m
			}
			continue
		}
		best[key] = m
		order = append(order, key)
	}
	out := make([]mountRec, 0, len(order))
	for _, k := range order {
		out = append(out, best[k])
	}
	return out
}

func readDisks() []disk {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return []disk{}
	}
	out := []disk{}
	for _, m := range pickDisks(parseMountinfo(string(data))) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(m.Mount, &st); err != nil || st.Blocks == 0 {
			continue
		}
		bs := uint64(st.Bsize)
		total := st.Blocks * bs
		out = append(out, disk{Mount: m.Mount, Device: m.Source, FS: m.FS, Total: total,
			Used: total - st.Bfree*bs, Avail: st.Bavail * bs})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}

type xbel struct {
	Bookmarks []struct {
		Href     string `xml:"href,attr"`
		Modified string `xml:"modified,attr"`
		Visited  string `xml:"visited,attr"`
	} `xml:"bookmark"`
}

type recentRef struct {
	Path string
	At   time.Time
}

func parseRecent(data []byte) []recentRef {
	var x xbel
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil
	}
	var out []recentRef
	for _, b := range x.Bookmarks {
		u, err := url.Parse(b.Href)
		if err != nil || u.Scheme != "file" || !filepath.IsAbs(u.Path) {
			continue
		}
		var at time.Time
		for _, s := range []string{b.Visited, b.Modified} {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				at = t
				break
			}
		}
		out = append(out, recentRef{filepath.Clean(u.Path), at})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

func hPlaces(ctx context.Context, c *rpc.Call) (any, error) {
	home, _ := homeDir()
	recent := []Entry{}
	if data, err := os.ReadFile(filepath.Join(home, ".local", "share", "recently-used.xbel")); err == nil {
		seen := map[string]bool{}
		for _, r := range parseRecent(data) {
			if seen[r.Path] {
				continue
			}
			seen[r.Path] = true
			e, err := statEntry(r.Path)
			if err != nil || e.Type == "dir" {
				continue
			}
			recent = append(recent, e)
			if len(recent) >= 40 {
				break
			}
		}
	}
	trashCount := 0
	if fd, _, err := trashDirs(); err == nil {
		if des, err := os.ReadDir(fd); err == nil {
			trashCount = len(des)
		}
	}
	return map[string]any{"home": home, "recent": recent, "disks": readDisks(), "trashCount": trashCount}, nil
}
