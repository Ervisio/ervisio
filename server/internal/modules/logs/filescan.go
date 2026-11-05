package logs

import (
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
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
		n := min(int64(bs), pos)
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
		end, more := emitBackward(buf, end, fn)
		if !more {
			return nil
		}
		carry = append([]byte(nil), buf[:end]...)
		if pos > 0 && maxBytes > 0 && total >= maxBytes {
			return nil
		}
	}
	fn(string(carry))
	return nil
}

// emitBackward calls fn with every complete line of buf[:end], last first, and
// returns the end of the unconsumed head. False means fn asked to stop.
func emitBackward(buf []byte, end int, fn func(string) bool) (int, bool) {
	for {
		i := bytes.LastIndexByte(buf[:end], '\n')
		if i < 0 {
			return end, true
		}
		if !fn(string(buf[i+1 : end])) {
			return end, false
		}
		end = i
	}
}

// scanFile reads a log file from the end and calls fn with each entry,
// newest first, until fn returns false, the file is exhausted, an entry older
// than sinceUs shows up or about maxBytes were read. Lines without a
// timestamp (stack traces, wrapped lines) take the timestamp and, when they
// have no level of their own, the level of the nearest earlier line that has
// one; the file's modification time is the last resort.
func scanFile(ctx context.Context, path, label, format string, maxBytes, sinceUs int64, fn func(*Entry) bool) error {
	f, st, err := openRegular(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
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
	// handle takes one parsed line; false stops the backward scan.
	handle := func(e *Entry, hasTs bool) bool {
		if !hasTs {
			pending = append(pending, e)
			return len(pending) < maxPending || flush(fallback, LvInfo)
		}
		if e.TsUs < sinceUs {
			pending = pending[:0]
			return false
		}
		if e.Level == "" {
			e.Level = LvInfo
		}
		return flush(e.TsUs, e.Level) && fn(e)
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
		if !handle(e, hasTs) {
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

// openRegular opens path and checks that it is a regular file.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, rpc.Errorf(rpc.Invalid, "%s is not a regular file", path)
	}
	return f, st, nil
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
			if lf, ok := candidateLog(d, p); ok {
				out = append(out, lf)
			}
		}
	}
	walk(root, 0)
	return out
}

// candidateLog reports whether the non-directory entry d at p is a readable
// (or admin-only) text log file.
func candidateLog(d os.DirEntry, p string) (LogFile, bool) {
	name := d.Name()
	if binaryLog[name] || rotatedRe.MatchString(name) {
		return LogFile{}, false
	}
	info, err := d.Info()
	if err != nil || !info.Mode().IsRegular() {
		return LogFile{}, false
	}
	lf := LogFile{Path: p, Size: info.Size()}
	fh, err := os.Open(p)
	if err != nil {
		lf.NeedsAdmin = os.IsPermission(err)
		return lf, lf.NeedsAdmin
	}
	head := make([]byte, 1024)
	n, _ := fh.Read(head)
	_ = fh.Close()
	return lf, bytes.IndexByte(head[:n], 0) < 0
}

type lineState struct {
	ts  int64
	lvl string
}

const (
	followPollInterval = 400 * time.Millisecond
	followReadSize     = 256 << 10
)

// fileFollower is the state of one followed file between polls.
type fileFollower struct {
	src     spec
	filter  filter
	out     chan<- Entry
	fh      *os.File
	ino     uint64
	pos     int64
	partial []byte
	last    lineState
	buf     []byte
}

// followFile polls a file for new lines (handling rotation and truncation)
// and sends the matching entries to out until ctx ends.
func followFile(ctx context.Context, s spec, f filter, out chan<- Entry) error {
	ff := &fileFollower{src: s, filter: f, out: out}
	defer ff.close()
	if err := ff.open(false); err != nil {
		return err
	}
	tick := time.NewTicker(followPollInterval)
	defer tick.Stop()
	ff.buf = make([]byte, followReadSize)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		if !ff.reopenIfRotated() {
			continue
		}
		if !ff.readNew(ctx) {
			return nil
		}
	}
}

func (ff *fileFollower) close() {
	if ff.fh != nil {
		_ = ff.fh.Close()
		ff.fh = nil
	}
}

// open (re)opens the file at its end, or at its start after a rotation.
func (ff *fileFollower) open(fromStart bool) error {
	ff.close()
	h, err := openShared(ff.src.Path)
	if err != nil {
		return err
	}
	info, err := h.Stat()
	if err != nil {
		_ = h.Close()
		return err
	}
	if !info.Mode().IsRegular() {
		_ = h.Close()
		return rpc.Errorf(rpc.Invalid, "%s is not a regular file", ff.src.Path)
	}
	ff.fh = h
	if id, ok := fileInode(ff.src.Path, info); ok {
		ff.ino = id
	}
	ff.pos = 0
	if !fromStart {
		ff.pos = info.Size()
	}
	ff.partial = nil
	return nil
}

// reopenIfRotated reopens the file from the start when it was replaced or
// truncated. False means there is nothing to read on this tick.
func (ff *fileFollower) reopenIfRotated() bool {
	info, err := os.Stat(ff.src.Path)
	if err != nil {
		return false // rotated away; wait for the new file
	}
	if id, ok := fileInode(ff.src.Path, info); ok && (id != ff.ino || info.Size() < ff.pos) {
		return ff.open(true) == nil
	}
	return true
}

// readNew reads everything appended since the last poll and sends its
// complete lines. False means ctx ended.
func (ff *fileFollower) readNew(ctx context.Context) bool {
	for {
		n, err := ff.fh.ReadAt(ff.buf, ff.pos)
		if n > 0 {
			ff.pos += int64(n)
			ff.partial = append(ff.partial, ff.buf[:n]...)
			if !ff.sendLines(ctx) {
				return false
			}
		}
		if err != nil || n == 0 {
			return true
		}
	}
}

// sendLines sends the matching complete lines buffered in partial and keeps
// the trailing incomplete one. False means ctx ended.
func (ff *fileFollower) sendLines(ctx context.Context) bool {
	for {
		i := bytes.IndexByte(ff.partial, '\n')
		if i < 0 {
			return true
		}
		line := strings.TrimRight(string(ff.partial[:i]), "\r")
		ff.partial = ff.partial[i+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		e := ff.entry(line)
		if !ff.filter.match(e) {
			continue
		}
		select {
		case ff.out <- *e:
		case <-ctx.Done():
			return false
		}
	}
}

// entry parses one line. A line without a timestamp takes the time and
// level of the last line that had one.
func (ff *fileFollower) entry(line string) *Entry {
	e, hasTs := lineEntry(line, ff.src.Label, ff.src.Path, 0, ff.src.Format, time.Now(), time.Local)
	if hasTs {
		ff.last = lineState{e.TsUs, e.Level}
		if e.Level == "" {
			e.Level, ff.last.lvl = LvInfo, LvInfo
		}
		return e
	}
	if ff.last.ts == 0 {
		ff.last.ts = nowUs()
	}
	e.TsUs, e.Ts = ff.last.ts, ff.last.ts/1000
	if e.Level == "" {
		e.Level = ff.last.lvl
		if e.Level == "" {
			e.Level = LvInfo
		}
	}
	return e
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
		data, more := emitForward(append(carry, buf[:n]...), fn)
		if !more {
			return nil
		}
		carry = append([]byte(nil), data...)
		if err != nil || n == 0 {
			if len(carry) > 0 {
				fn(string(carry))
			}
			if err == io.EOF {
				err = nil
			}
			return err
		}
	}
}

// emitForward calls fn with every complete line of data and returns the
// remaining tail. False means fn asked to stop.
func emitForward(data []byte, fn func(string) bool) ([]byte, bool) {
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return data, true
		}
		if !fn(string(data[:i])) {
			return data, false
		}
		data = data[i+1:]
	}
}
