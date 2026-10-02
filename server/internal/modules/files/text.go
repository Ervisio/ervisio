package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

const (
	defaultReadText = 1 << 20
	maxReadText     = 8 << 20
)

// decodeText turns raw bytes into a string and names the encoding.
// It refuses binary data.
func decodeText(b []byte, truncated bool) (string, string, error) {
	probe := b
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	if bytes.IndexByte(probe, 0) >= 0 || bytes.HasPrefix(b, []byte{0xFF, 0xFE}) || bytes.HasPrefix(b, []byte{0xFE, 0xFF}) {
		return "", "", rpc.Errorf(rpc.Invalid, "This looks like a binary file, so it cannot be shown as text.")
	}
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		b = b[3:]
		if truncated {
			b = trimPartialRune(b)
		}
		if utf8.Valid(b) {
			return string(b), "utf-8-bom", nil
		}
	}
	if truncated {
		b = trimPartialRune(b)
	}
	if utf8.Valid(b) {
		return string(b), "utf-8", nil
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r), "latin1", nil
}

func trimPartialRune(b []byte) []byte {
	for i := 1; i <= 3 && i <= len(b); i++ {
		c := b[len(b)-i]
		if c&0xC0 == 0xC0 { // start byte
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			break
		}
		if c&0x80 == 0 {
			break
		}
	}
	return b
}

func encodeText(s, enc string) ([]byte, error) {
	switch enc {
	case "", "utf-8":
		return []byte(s), nil
	case "utf-8-bom":
		return append([]byte{0xEF, 0xBB, 0xBF}, s...), nil
	case "latin1":
		out := make([]byte, 0, len(s))
		for _, r := range s {
			if r > 255 {
				return nil, rpc.Errorf(rpc.Invalid, "The text contains characters that this file's encoding (Latin-1) cannot store.")
			}
			out = append(out, byte(r))
		}
		return out, nil
	}
	return nil, rpc.Errorf(rpc.Invalid, "Unknown encoding %q.", enc)
}

func hReadText(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path     string `json:"path"`
		MaxBytes int64  `json:"maxBytes"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	max := p.MaxBytes
	if max <= 0 {
		max = defaultReadText
	}
	if max > maxReadText {
		max = maxReadText
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be opened as text.")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	truncated := int64(len(buf)) > max
	if truncated {
		buf = buf[:max]
	}
	content, enc, err := decodeText(buf, truncated)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path": path, "content": content, "encoding": enc, "size": fi.Size(),
		"mtime": fi.ModTime().UnixMilli(), "truncated": truncated,
	}, nil
}

func hWriteText(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Path          string `json:"path"`
		Content       string `json:"content"`
		Encoding      string `json:"encoding"`
		ExpectedMtime int64  `json:"expectedMtime"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	path, err := cleanPath(p.Path)
	if err != nil {
		return nil, err
	}
	data, err := encodeText(p.Content, p.Encoding)
	if err != nil {
		return nil, err
	}
	if len(data) > maxReadText*2 {
		return nil, rpc.Errorf(rpc.Invalid, "The text is too large to save here (limit 16 MiB).")
	}
	return writeTextFile(path, data, p.ExpectedMtime)
}

// resolveFinal follows the links at the end of path, so saving a link saves
// the file it points to. In the root bridge only links owned by root are
// followed; each hop is resolved through directory descriptors.
func resolveFinal(path string) (string, error) {
	if !strictLinks {
		if r, err := filepath.EvalSymlinks(path); err == nil {
			return r, nil
		}
		return path, nil
	}
	for i := 0; i < maxLinkHops; i++ {
		d, name, err := openParent(path)
		if err != nil {
			return "", err
		}
		fd, err := unix.Openat(dfd(d), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		d.Close()
		if errors.Is(err, unix.ENOENT) {
			return path, nil
		}
		if err != nil {
			return "", pathErr("open", path, err)
		}
		var st unix.Stat_t
		err = unix.Fstat(fd, &st)
		if err != nil || !isLink(&st) {
			unix.Close(fd)
			return path, pathErr("stat", path, err)
		}
		if st.Uid != trustedLinkUID {
			unix.Close(fd)
			return "", errUntrustedLink(path)
		}
		t, err := readlinkAt(fd, "")
		unix.Close(fd)
		if err != nil {
			return "", pathErr("readlink", path, err)
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(path), t)
		}
		path = filepath.Clean(t)
	}
	return "", pathErr("open", path, unix.ELOOP)
}

func writeTextFile(path string, data []byte, expected int64) (any, error) {
	path, err := resolveFinal(path)
	if err != nil {
		return nil, err
	}
	d, name, err := openParent(path)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	st, err := lstatAt(dfd(d), name)
	exists := err == nil
	if err != nil && !errors.Is(err, unix.ENOENT) {
		return nil, pathErr("lstat", path, err)
	}
	if exists {
		if !isReg(&st) {
			return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be saved as text.")
		}
		mt := time.Unix(st.Mtim.Unix()).UnixMilli()
		if expected != 0 && mt != expected {
			return nil, rpc.Errorf(rpc.Conflict, "This file was changed by someone else since you opened it. Reload it, or copy your text first.").
				WithData(map[string]any{"mtime": mt})
		}
	} else if expected != 0 {
		return nil, rpc.Errorf(rpc.Conflict, "This file was removed since you opened it.")
	}
	perm := uint32(0o644)
	if exists {
		perm = st.Mode & 0o7777
	}
	tmp, tname, terr := createTempAt(dfd(d), ".save-", filepath.Dir(path))
	if terr == nil {
		tfd := int(tmp.Fd())
		_, werr := tmp.Write(data)
		if werr == nil && exists && os.Geteuid() == 0 {
			_ = unix.Fchown(tfd, int(st.Uid), int(st.Gid)) // before chmod: chown clears setuid bits
		}
		if werr == nil {
			werr = unix.Fchmod(tfd, perm)
		}
		cerr := tmp.Close()
		if werr == nil && cerr == nil {
			// renameat replaces whatever is at name now, never what a link points to.
			rerr := unix.Renameat(dfd(d), tname, dfd(d), name)
			if rerr == nil {
				return saved(path)
			}
			werr = pathErr("rename", path, rerr)
		}
		_ = unix.Unlinkat(dfd(d), tname, 0)
		if werr == nil {
			werr = cerr
		}
		terr = werr
	}
	// The folder may not be writable although the file is: write in place.
	if exists && os.IsPermission(terr) {
		fd, err := unix.Openat(dfd(d), name, unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, pathErr("open", path, err)
		}
		f := os.NewFile(uintptr(fd), path)
		var now unix.Stat_t
		if err := unix.Fstat(fd, &now); err != nil || !isReg(&now) {
			f.Close()
			return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be saved as text.")
		}
		err = unix.Ftruncate(fd, 0)
		if err == nil {
			_, err = f.Write(data)
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, pathErr("write", path, err)
		}
		return saved(path)
	}
	return nil, terr
}

func saved(path string) (any, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": path, "size": fi.Size(), "mtime": fi.ModTime().UnixMilli()}, nil
}
