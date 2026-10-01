package files

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	"github.com/Fonlogen/LinuxAdmin/server/internal/rpc"
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

func writeTextFile(path string, data []byte, expected int64) (any, error) {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		path = r
	}
	fi, err := os.Stat(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if exists {
		if !fi.Mode().IsRegular() {
			return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be saved as text.")
		}
		if expected != 0 && fi.ModTime().UnixMilli() != expected {
			return nil, rpc.Errorf(rpc.Conflict, "This file was changed by someone else since you opened it. Reload it, or copy your text first.").
				WithData(map[string]any{"mtime": fi.ModTime().UnixMilli()})
		}
	} else if expected != 0 {
		return nil, rpc.Errorf(rpc.Conflict, "This file was removed since you opened it.")
	}
	perm := os.FileMode(0o644)
	if exists {
		perm = fi.Mode()
	}
	dir := filepath.Dir(path)
	tmp, terr := os.CreateTemp(dir, ".save-*")
	if terr == nil {
		name := tmp.Name()
		_, werr := tmp.Write(data)
		cerr := tmp.Close()
		if werr == nil && cerr == nil {
			werr = os.Chmod(name, perm&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky))
		}
		if werr == nil && cerr == nil && exists {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
				_ = os.Lchown(name, int(st.Uid), int(st.Gid))
			}
		}
		if werr == nil && cerr == nil {
			if rerr := os.Rename(name, path); rerr == nil {
				return saved(path)
			} else {
				werr = rerr
			}
		}
		os.Remove(name)
		if werr == nil {
			werr = cerr
		}
		terr = werr
	}
	// The folder may not be writable although the file is: write in place.
	if exists && os.IsPermission(terr) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return nil, err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
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
