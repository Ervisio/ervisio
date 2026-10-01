package logs

import (
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
)

const (
	maxLineCountSize = 128 << 20 // files larger than this get no line numbers
	maxScanBytes     = 96 << 20  // upper bound of bytes read backwards per query
	maxPending       = 100000
)

// countLines returns the number of lines in the first size bytes of f.
func countLines(ctx context.Context, f *os.File, size int64) (int, error) {
	if size == 0 {
		return 0, nil
	}
	buf := make([]byte, 1<<20)
	n := 0
	var off int64
	for off < size {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		want := int64(len(buf))
		if size-off < want {
			want = size - off
		}
		r, err := f.ReadAt(buf[:want], off)
		n += bytes.Count(buf[:r], []byte{'\n'})
		off += int64(r)
		if err != nil && err != io.EOF {
			return 0, err
		}
		if r == 0 {
			break
		}
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, size-1); err == nil && last[0] != '\n' {
		n++
	}
	return n, nil
}

// backwardLines calls fn with every line of the first size bytes of f, last
// line first (empty lines included, "\r" not removed). It stops when fn
// returns false or after about maxBytes bytes have been read (0 = no limit).
func backwardLines(ctx context.Context, f *os.File, size, maxBytes int64, fn func(string) bool) error {
	if size == 0 {
		return nil
	}
	const bs = 128 << 10
	pos := size
	var carry []byte
	first := true
	var total int64
	for pos > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := int64(bs)
		if pos < n {
			n = pos
		}
		pos -= n
		buf := make([]byte, int(n)+len(carry))
		if _, err := f.ReadAt(buf[:n], pos); err != nil && err != io.EOF {
			return err
		}
		copy(buf[n:], carry)
		total += n
		end := len(buf)
		if first {
			first = false
			if buf[end-1] == '\n' {
				end--
			}
		}
		for {
			i := bytes.LastIndexByte(buf[:end], '\n')
			if i < 0 {
				break
			}
			if !fn(string(buf[i+1 : end])) {
				return nil
			}
			end = i
		}
		carry = append([]byte(nil), buf[:end]...)
		if pos > 0 && maxBytes > 0 && total >= maxBytes {
			return nil
		}
	}
	fn(string(carry))
	return nil
}

// scanFile reads a log file from the end and calls fn with each entry,
// newest first, until fn returns false, the file is exhausted, an entry older
// than sinceUs shows up or about maxBytes were read. Lines without a
// timestamp (stack traces, wrapped lines) take the timestamp and, when they
// have no level of their own, the level of the nearest earlier line that has
// one; the file's modification time is the last resort.
func scanFile(ctx context.Context, path, label, format string, maxBytes, sinceUs int64, fn func(*Entry) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return rpc.Errorf(rpc.Invalid, "%s is not a regular file", path)
	}
	size := st.Size()
	known := false
	no := 0
	if size <= maxLineCountSize {
		if no, err = countLines(ctx, f, size); err != nil {
			return err
		}
		known = true
	}
	fallback := st.ModTime().UnixMicro()
	now, loc := time.Now(), time.Local
	var pending []*Entry
	stopped := false
	flush := func(ts int64, lvl string) bool {
		for _, p := range pending {
			p.TsUs, p.Ts = ts, ts/1000
			if p.Level == "" {
				p.Level = lvl
			}
			if !fn(p) {
				return false
			}
		}
		pending = pending[:0]
		return true
	}
	err = backwardLines(ctx, f, size, maxBytes, func(text string) bool {
		n := no
		no--
		text = strings.TrimRight(text, "\r")
		if strings.TrimSpace(text) == "" {
			return true
		}
		if !known {
			n = 0
		}
		e, hasTs := lineEntry(text, label, path, n, format, now, loc)
		if !hasTs {
			pending = append(pending, e)
			if len(pending) >= maxPending {
				if !flush(fallback, LvInfo) {
					stopped = true
					return false
				}
			}
			return true
		}
		if e.TsUs < sinceUs {
			pending = pending[:0]
			stopped = true
			return false
		}
		if e.Level == "" {
			e.Level = LvInfo
		}
		if !flush(e.TsUs, e.Level) || !fn(e) {
			stopped = true
			return false
		}
		return true
	})
	if err != nil || stopped {
		return err
	}
	if sinceUs <= fallback {
		flush(fallback, LvInfo)
	}
	return nil
}

// fetchFile returns up to want matching entries with TsUs <= until, newest first.
func fetchFile(ctx context.Context, s spec, f filter, until int64, want int) ([]Entry, error) {
	var out []Entry
	err := scanFile(ctx, s.Path, s.Label, s.Format, maxScanBytes, f.sinceUs, func(e *Entry) bool {
		if e.TsUs > until || !f.match(e) {
			return true
		}
		out = append(out, *e)
		return len(out) < want
	})
	return out, err
}

// ---------- listing /var/log ----------

var (
	rotatedRe = regexp.MustCompile(`(\.(gz|xz|zst|bz2|zip|old|journal~?|bak|tmp)|\.\d+|-\d{6,8}|\.\d+\.(gz|xz|zst|bz2))$`)
	binaryLog = map[string]bool{"wtmp": true, "btmp": true, "utmp": true, "lastlog": true, "faillog": true, "tallylog": true}
)

// LogFile is a candidate log file under /var/log.
type LogFile struct {
	Path       string
	Size       int64
	NeedsAdmin bool
}

// listLogFiles lists log-like files below root (depth <= 3).
func listLogFiles(root string, max int) []LogFile {
	var out []LogFile
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, d := range ents {
			if len(out) >= max {
				return
			}
			p := dir + "/" + d.Name()
			name := d.Name()
			if d.IsDir() {
				if depth < 3 && name != "journal" && name != "private" {
					walk(p, depth+1)
				}
				continue
			}
			if binaryLog[name] || rotatedRe.MatchString(name) {
				continue
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			lf := LogFile{Path: p, Size: info.Size()}
			fh, err := os.Open(p)
			if err != nil {
				if !os.IsPermission(err) {
					continue
				}
				lf.NeedsAdmin = true
			} else {
				head := make([]byte, 1024)
				n, _ := fh.Read(head)
				fh.Close()
				if bytes.IndexByte(head[:n], 0) >= 0 {
					continue
				}
			}
			out = append(out, lf)
		}
	}
	walk(root, 0)
	return out
}

// ---------- following ----------

type lineState struct {
	ts  int64
	lvl string
}

// followFile polls a file for new lines (handling rotation and truncation)
// and sends the matching entries to out until ctx ends.
func followFile(ctx context.Context, s spec, f filter, out chan<- Entry) error {
	var (
		fh      *os.File
		ino     uint64
		pos     int64
		partial []byte
		st      lineState
	)
	defer func() {
		if fh != nil {
			fh.Close()
		}
	}()
	open := func(fromStart bool) error {
		if fh != nil {
			fh.Close()
			fh = nil
		}
		h, err := os.Open(s.Path)
		if err != nil {
			return err
		}
		info, err := h.Stat()
		if err != nil {
			h.Close()
			return err
		}
		if !info.Mode().IsRegular() {
			h.Close()
			return rpc.Errorf(rpc.Invalid, "%s is not a regular file", s.Path)
		}
		fh = h
		if sys, ok := info.Sys().(*syscall.Stat_t); ok {
			ino = sys.Ino
		}
		pos = 0
		if !fromStart {
			pos = info.Size()
		}
		partial = nil
		return nil
	}
	if err := open(false); err != nil {
		return err
	}
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	buf := make([]byte, 256<<10)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		info, err := os.Stat(s.Path)
		if err != nil {
			continue // rotated away; wait for the new file
		}
		if sy, ok := info.Sys().(*syscall.Stat_t); ok && (sy.Ino != ino || info.Size() < pos) {
			if open(true) != nil {
				continue
			}
		}
		for {
			n, err := fh.ReadAt(buf, pos)
			if n > 0 {
				pos += int64(n)
				partial = append(partial, buf[:n]...)
				for {
					i := bytes.IndexByte(partial, '\n')
					if i < 0 {
						break
					}
					line := strings.TrimRight(string(partial[:i]), "\r")
					partial = partial[i+1:]
					if strings.TrimSpace(line) == "" {
						continue
					}
					e, hasTs := lineEntry(line, s.Label, s.Path, 0, s.Format, time.Now(), time.Local)
					if hasTs {
						st = lineState{e.TsUs, e.Level}
						if e.Level == "" {
							e.Level, st.lvl = LvInfo, LvInfo
						}
					} else {
						if st.ts == 0 {
							st.ts = nowUs()
						}
						e.TsUs, e.Ts = st.ts, st.ts/1000
						if e.Level == "" {
							e.Level = st.lvl
							if e.Level == "" {
								e.Level = LvInfo
							}
						}
					}
					if !f.match(e) {
						continue
					}
					select {
					case out <- *e:
					case <-ctx.Done():
						return nil
					}
				}
			}
			if err != nil || n == 0 {
				break
			}
		}
	}
}

// forwardLines calls fn with each line of f from the start until it returns false.
func forwardLines(ctx context.Context, f *os.File, fn func(string) bool) error {
	buf := make([]byte, 128<<10)
	var carry []byte
	var pos int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := f.ReadAt(buf, pos)
		pos += int64(n)
		data := append(carry, buf[:n]...)
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			if !fn(string(data[:i])) {
				return nil
			}
			data = data[i+1:]
		}
		carry = append([]byte(nil), data...)
		if err != nil || n == 0 {
			if len(carry) > 0 {
				fn(string(carry))
			}
			if err == io.EOF || err == nil {
				return nil
			}
			return err
		}
	}
}
