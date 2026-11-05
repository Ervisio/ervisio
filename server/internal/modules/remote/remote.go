// Package remote lets the Files section browse SFTP, FTP(S), SMB and
// WebDAV servers (docs/api/remote.md).
//
// It runs in the user bridge: connections are opened with the rights of
// the signed-in account, which also owns the saved connections
// (~/.config/ervisio/remotes.json). Passwords and keys never travel back
// to the browser.
package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/ervisio/ervisio/server/internal/modules/files"
	"github.com/ervisio/ervisio/server/internal/rpc"
)

type module struct {
	st   *store
	pool *pool
}

// Register adds the remote.* methods.
func Register(r *rpc.Registry) {
	st := &store{}
	register(r, &module{st: st, pool: newPool(st)})
}

func register(r *rpc.Registry, m *module) {
	r.Handle("remote.list", rpc.User, m.hList)
	r.Handle("remote.save", rpc.User, m.hSave)
	r.Handle("remote.forget", rpc.User, m.hForget)
	r.Handle("remote.test", rpc.User, m.hTest)
	r.Handle("remote.start", rpc.User, m.hStart)
	r.Handle("remote.ls", rpc.User, m.hLs)
	r.Handle("remote.stat", rpc.User, m.hStat)
	r.Handle("remote.mkdir", rpc.User, m.hMkdir)
	r.Handle("remote.create", rpc.User, m.hCreate)
	r.Handle("remote.rename", rpc.User, m.hRename)
	r.Handle("remote.remove", rpc.User, m.hRemove)
	r.Handle("remote.readText", rpc.User, m.hReadText)
	r.Handle("remote.writeText", rpc.User, m.hWriteText)
	r.Stream("remote.readStream", rpc.User, m.hReadStream)
	r.Stream("remote.writeStream", rpc.User, m.hWriteStream)
	r.Stream("remote.copy", rpc.User, m.hCopy)
}

// remoteErr names an error for the browser. Permission errors become
// forbidden, never needs_admin: administrator rights on this computer do
// not help on another server.
func remoteErr(err error) error {
	if err == nil {
		return nil
	}
	var re *rpc.Error
	var hk *hostKeyError
	switch {
	case errors.As(err, &re):
		return re
	case errors.As(err, &hk):
		return rpc.Errorf(rpc.Conflict, "%s", hk.Error()).WithData(map[string]any{"reason": "hostKey", "fingerprint": fingerprint(hk.Key), "changed": hk.Changed})
	case errors.Is(err, fs.ErrNotExist):
		return rpc.Errorf(rpc.NotFound, "%s", cleanMsg(err, "It does not exist (any more)."))
	case errors.Is(err, fs.ErrExist):
		return rpc.Errorf(rpc.Conflict, "%s", cleanMsg(err, "It already exists."))
	case errors.Is(err, fs.ErrPermission):
		return rpc.Errorf(rpc.Forbidden, "%s", cleanMsg(err, "The server does not allow this."))
	case errors.Is(err, context.Canceled):
		return rpc.Errorf(rpc.Unavailable, "The operation was cancelled.")
	case connBroken(err):
		return rpc.Errorf(rpc.Unavailable, "The connection to the server was lost (%v). Try again.", err)
	}
	return rpc.Errorf(rpc.Internal, "%v", err)
}

// cleanMsg keeps the server's wording when there is some.
func cleanMsg(err error, fallback string) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		inner := pe.Err.Error()
		if inner == fs.ErrNotExist.Error() || inner == fs.ErrExist.Error() || inner == fs.ErrPermission.Error() || strings.HasPrefix(inner, "sftp:") {
			return pe.Path + ": " + fallback
		}
		return pe.Path + ": " + inner
	}
	var de *davStatusError
	if errors.As(err, &de) {
		return de.Error()
	}
	return fallback
}

/* ---------- connections ---------- */

func (m *module) hList(ctx context.Context, c *rpc.Call) (any, error) {
	list, err := m.st.list()
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(list))
	for _, x := range list {
		out = append(out, x.view())
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return map[string]any{"connections": out}, nil
}

func (m *module) specFrom(c *rpc.Call) (*Conn, bool, error) {
	var p specParams
	if err := c.Bind(&p); err != nil {
		return nil, false, err
	}
	var old *Conn
	if p.ID != "" {
		if !validID(p.ID) {
			return nil, false, rpc.Errorf(rpc.Invalid, "The connection id is not valid.")
		}
		o, err := m.st.get(p.ID)
		if err != nil {
			return nil, false, err
		}
		old = o
	}
	spec, err := build(p, old)
	return spec, old != nil, err
}

func (m *module) hSave(ctx context.Context, c *rpc.Call) (any, error) {
	spec, _, err := m.specFrom(c)
	if err != nil {
		return nil, err
	}
	if spec.Kind == KindSFTP && spec.HostKey == "" {
		return nil, rpc.Errorf(rpc.Invalid, "Test the connection first and trust the server's host key.")
	}
	if spec.ID == "" {
		spec.ID = newID()
	}
	if err := m.st.put(spec); err != nil {
		return nil, err
	}
	m.pool.drop(spec.ID)
	return spec.view(), nil
}

func (m *module) hForget(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if !validID(p.ID) {
		return nil, rpc.Errorf(rpc.Invalid, "The connection id is not valid.")
	}
	if err := m.st.remove(p.ID); err != nil {
		return nil, err
	}
	m.pool.drop(p.ID)
	return map[string]bool{"ok": true}, nil
}

// startPath is the folder a connection opens in.
func startPath(f FS, spec *Conn) string {
	if spec.Path != "" {
		return spec.Path
	}
	if s, ok := f.(*sftpFS); ok {
		return s.home()
	}
	if s, ok := f.(*ftpFS); ok {
		if p, err := s.c.CurrentDir(); err == nil && strings.HasPrefix(p, "/") {
			return path.Clean(p)
		}
	}
	return "/"
}

// hTest connects with unsaved settings. For SFTP without a pinned key it
// stops before signing in and returns the server's key to be confirmed.
func (m *module) hTest(ctx context.Context, c *rpc.Call) (any, error) {
	spec, _, err := m.specFrom(c)
	if err != nil {
		return nil, err
	}
	f, err := m.pool.dial(ctx, spec)
	if err != nil {
		var hk *hostKeyError
		if errors.As(err, &hk) {
			return map[string]any{"ok": false, "needsTrust": true, "changed": hk.Changed, "fingerprint": fingerprint(hk.Key), "hostKey": formatHostKey(hk.Key), "keyType": hk.Key.Type()}, nil
		}
		return nil, remoteErr(err)
	}
	defer f.Close()
	sp := startPath(f, spec)
	if _, err := f.List(ctx, sp); err != nil {
		return nil, rpc.Errorf(rpc.Invalid, "Signed in, but the folder %s cannot be opened: %v", sp, remoteErr(err).(*rpc.Error).Message)
	}
	return map[string]any{"ok": true, "path": sp}, nil
}

func (m *module) hStart(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Conn string `json:"conn"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	var sp string
	err := m.pool.with(ctx, p.Conn, func(h *handle) error {
		sp = startPath(h.FS, h.spec)
		return nil
	})
	if err != nil {
		return nil, remoteErr(err)
	}
	return map[string]string{"path": sp}, nil
}

/* ---------- file operations ---------- */

type pathParams struct {
	Conn string `json:"conn"`
	Path string `json:"path"`
}

func (m *module) run(ctx context.Context, id string, fn func(h *handle) error) error {
	return remoteErr(m.pool.with(ctx, id, fn))
}

func (m *module) hLs(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		pathParams
		ShowHidden bool `json:"showHidden"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	dir, err := cleanRemote(p.Path)
	if err != nil {
		return nil, err
	}
	var list []Entry
	err = m.run(ctx, p.Conn, func(h *handle) error {
		var err error
		list, err = h.List(ctx, dir)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(list))
	for _, e := range list {
		if !p.ShowHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		e.Path = ""
		out = append(out, e)
	}
	truncated := false
	if len(out) > maxList {
		out, truncated = out[:maxList], true
	}
	parent := ""
	if dir != "/" {
		parent = path.Dir(dir)
	}
	return map[string]any{"path": dir, "entries": out, "parent": parent, "truncated": truncated}, nil
}

func (m *module) hStat(ctx context.Context, c *rpc.Call) (any, error) {
	var p pathParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return nil, err
	}
	var e Entry
	err = m.run(ctx, p.Conn, func(h *handle) error {
		var err error
		e, err = h.Stat(ctx, pth)
		return err
	})
	if err != nil {
		return nil, err
	}
	e.Path = pth
	return e, nil
}

func (m *module) hMkdir(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		pathParams
		Parents bool `json:"parents"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return nil, err
	}
	if pth == "/" {
		return nil, rpc.Errorf(rpc.Conflict, "/ already exists.")
	}
	if err := validName(path.Base(pth)); err != nil {
		return nil, err
	}
	err = m.run(ctx, p.Conn, func(h *handle) error {
		if !p.Parents {
			return mkdirOne(ctx, h, pth)
		}
		return mkdirParents(ctx, h, pth)
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": pth}, nil
}

func mkdirOne(ctx context.Context, h *handle, pth string) error {
	if ok, err := exists(ctx, h, pth); err != nil {
		return err
	} else if ok {
		return rpc.Errorf(rpc.Conflict, "%s already exists.", path.Base(pth))
	}
	return h.Mkdir(ctx, pth)
}

// mkdirParents makes every missing parent; an existing folder is a conflict
// only at the end (like mkdir -p reports nothing, files.mkdir does).
func mkdirParents(ctx context.Context, h *handle, pth string) error {
	var todo []string
	for d := pth; d != "/"; d = path.Dir(d) {
		e, err := h.Stat(ctx, d)
		if err == nil {
			if !isDirLike(e) {
				return rpc.Errorf(rpc.Invalid, "%s is not a folder.", d)
			}
			if d == pth {
				return rpc.Errorf(rpc.Conflict, "%s already exists.", path.Base(pth))
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		todo = append(todo, d)
	}
	for i := len(todo) - 1; i >= 0; i-- {
		if err := h.Mkdir(ctx, todo[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *module) hCreate(ctx context.Context, c *rpc.Call) (any, error) {
	var p pathParams
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return nil, err
	}
	err = m.run(ctx, p.Conn, func(h *handle) error {
		_, err := writeFile(ctx, h, posixOps, pth, strings.NewReader(""), 0, false, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": pth}, nil
}

func (m *module) hRename(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Conn string `json:"conn"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	from, to, err := cleanRenamePaths(p.From, p.To)
	if err != nil {
		return nil, err
	}
	if from == to {
		return map[string]string{"path": to}, nil
	}
	err = m.run(ctx, p.Conn, func(h *handle) error {
		return renameChecked(ctx, h, from, to)
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"path": to}, nil
}

func cleanRenamePaths(rawFrom, rawTo string) (from, to string, err error) {
	if from, err = cleanRemote(rawFrom); err != nil {
		return "", "", err
	}
	if to, err = cleanRemote(rawTo); err != nil {
		return "", "", err
	}
	if from == "/" || to == "/" {
		return "", "", rpc.Errorf(rpc.Invalid, "The top folder cannot be renamed.")
	}
	if err := validName(path.Base(to)); err != nil {
		return "", "", err
	}
	if from != to && strings.HasPrefix(to, from+"/") {
		return "", "", rpc.Errorf(rpc.Invalid, "A folder cannot be moved into itself.")
	}
	return from, to, nil
}

func renameChecked(ctx context.Context, h *handle, from, to string) error {
	if ok, err := exists(ctx, h, to); err != nil {
		return err
	} else if ok && !strings.EqualFold(from, to) {
		return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", path.Base(to))
	}
	return h.Rename(ctx, from, to)
}

func (m *module) hRemove(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		Conn  string   `json:"conn"`
		Paths []string `json:"paths"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	if len(p.Paths) == 0 {
		return nil, rpc.Errorf(rpc.Invalid, "Nothing was selected.")
	}
	if len(p.Paths) > 10000 {
		return nil, rpc.Errorf(rpc.Invalid, "Too many items at once (limit 10000).")
	}
	type failure struct {
		Path    string `json:"path"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	deleted := []string{}
	failed := []failure{}
	err := m.run(ctx, p.Conn, func(h *handle) error {
		for _, raw := range p.Paths {
			pth, err := cleanRemote(raw)
			if err == nil {
				err = removeAll(ctx, h, pth)
			}
			if err != nil {
				if connBroken(err) {
					return err
				}
				re := remoteErr(err).(*rpc.Error)
				failed = append(failed, failure{Path: raw, Code: string(re.Code), Message: re.Message})
				continue
			}
			deleted = append(deleted, pth)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": deleted, "failed": failed}, nil
}

/* ---------- text ---------- */

func (m *module) hReadText(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		pathParams
		MaxBytes int64 `json:"maxBytes"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return nil, err
	}
	max := clampReadMax(p.MaxBytes)
	var out map[string]any
	err = m.run(ctx, p.Conn, func(h *handle) error {
		var err error
		out, err = readTextFile(ctx, h, pth, max)
		return err
	})
	return out, err
}

func clampReadMax(n int64) int64 {
	if n <= 0 {
		n = 1 << 20
	}
	if n > files.MaxReadText {
		n = files.MaxReadText
	}
	return n
}

func readTextFile(ctx context.Context, h *handle, pth string, max int64) (map[string]any, error) {
	e, err := h.Stat(ctx, pth)
	if err != nil {
		return nil, err
	}
	if !isFileLike(e) {
		return nil, rpc.Errorf(rpc.Invalid, "Only regular files can be opened as text.")
	}
	r, err := h.Open(ctx, pth)
	if err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(io.LimitReader(r, max+1))
	r.Close()
	if err != nil {
		return nil, err
	}
	truncated := int64(len(buf)) > max
	if truncated {
		buf = buf[:max]
	}
	content, enc, err := files.DecodeText(buf, truncated)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": pth, "content": content, "encoding": enc, "size": e.Size, "mtime": e.MTime, "truncated": truncated}, nil
}

func (m *module) hWriteText(ctx context.Context, c *rpc.Call) (any, error) {
	var p struct {
		pathParams
		Content       string `json:"content"`
		Encoding      string `json:"encoding"`
		ExpectedMtime int64  `json:"expectedMtime"`
	}
	if err := c.Bind(&p); err != nil {
		return nil, err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return nil, err
	}
	data, err := files.EncodeText(p.Content, p.Encoding)
	if err != nil {
		return nil, err
	}
	if len(data) > files.MaxReadText*2 {
		return nil, rpc.Errorf(rpc.Invalid, "The text is too large to save here (limit 16 MiB).")
	}
	var out map[string]any
	err = m.run(ctx, p.Conn, func(h *handle) error {
		var err error
		out, err = writeTextFile(ctx, h, pth, data, p.ExpectedMtime)
		return err
	})
	return out, err
}

func writeTextFile(ctx context.Context, h *handle, pth string, data []byte, expectedMtime int64) (map[string]any, error) {
	e, err := h.Stat(ctx, pth)
	switch {
	case err == nil:
		if expectedMtime != 0 && e.MTime != expectedMtime {
			return nil, rpc.Errorf(rpc.Conflict, "The file changed on the server since it was opened.")
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	if _, err := writeFile(ctx, h, posixOps, pth, strings.NewReader(string(data)), int64(len(data)), true, nil); err != nil {
		return nil, err
	}
	ne, err := h.Stat(ctx, pth)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": pth, "size": ne.Size, "mtime": ne.MTime}, nil
}

/* ---------- streams (docs/api/files-transfer.md) ---------- */

func (m *module) hReadStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p pathParams
	if err := c.Bind(&p); err != nil {
		return err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return err
	}
	h, err := m.pool.get(ctx, p.Conn)
	if err != nil {
		return remoteErr(err)
	}
	err = readStream(ctx, h, pth, s)
	h.release(err)
	return remoteErr(err)
}

func readStream(ctx context.Context, h *handle, pth string, s rpc.Stream) error {
	e, err := h.Stat(ctx, pth)
	if err != nil {
		return err
	}
	if !isFileLike(e) {
		return rpc.Errorf(rpc.Invalid, "Only files can be downloaded. Select a file, not a folder.")
	}
	if e.Type == "symlink" {
		if ti, err := statFollow(ctx, h, pth); err == nil {
			e.Size = ti.Size
		}
	}
	r, err := h.Open(ctx, pth)
	if err != nil {
		return err
	}
	defer r.Close()
	buf := make([]byte, 64<<10)
	n, rerr := readChunk(r, buf)
	if rerr != nil && rerr != io.EOF {
		return rerr
	}
	mt := mimeOf(e.Name)
	if mt == "" {
		mt = http.DetectContentType(buf[:n])
	}
	if err := s.Send(map[string]any{"name": e.Name, "size": e.Size, "mime": mt}); err != nil {
		return err
	}
	sent, err := sendChunks(ctx, r, s, buf, n, rerr)
	if err != nil {
		return err
	}
	if sent != e.Size {
		return rpc.Errorf(rpc.Internal, "The file changed while it was being read (%d of %d bytes).", sent, e.Size)
	}
	return s.Send(map[string]bool{"done": true})
}

// readChunk fills buf; a short read is reported as io.EOF, a real error as is.
func readChunk(r io.Reader, buf []byte) (int, error) {
	n, err := io.ReadFull(r, buf)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}

// sendChunks streams the first n bytes of buf, then keeps reading until EOF.
func sendChunks(ctx context.Context, r io.Reader, s rpc.Stream, buf []byte, n int, rerr error) (int64, error) {
	var sent int64
	for {
		if n > 0 {
			if err := s.SendBytes(buf[:n]); err != nil {
				return sent, err
			}
			sent += int64(n)
		}
		if rerr != nil {
			return sent, nil
		}
		if err := ctx.Err(); err != nil {
			return sent, err
		}
		n, rerr = readChunk(r, buf)
		if rerr != nil && rerr != io.EOF {
			return sent, rerr
		}
	}
}

// statFollow describes what a symlink points to (one level, via its parent
// listing would be costly: protocols with a Stat that follows use it).
func statFollow(ctx context.Context, h *handle, p string) (Entry, error) {
	if s, ok := h.FS.(*sftpFS); ok {
		fi, err := s.c.Stat(p)
		if err != nil {
			return Entry{}, err
		}
		return infoEntry(path.Dir(p), fi), nil
	}
	return h.Stat(ctx, p)
}

// inputReader reads {"data":b64} inputs until {"eof":true}.
type inputReader struct {
	ctx context.Context
	s   rpc.Stream
	buf []byte
	eof bool
}

var errEarlyEnd = errors.New("the upload stopped before the end")

func (r *inputReader) Read(b []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.eof {
			return 0, io.EOF
		}
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case in, open := <-r.s.Input():
			if !open {
				return 0, errEarlyEnd
			}
			var msg struct {
				Data string `json:"data"`
				EOF  bool   `json:"eof"`
			}
			if err := json.Unmarshal(in, &msg); err != nil {
				return 0, rpc.Errorf(rpc.Invalid, "Malformed upload data.")
			}
			if msg.EOF {
				r.eof = true
				continue
			}
			d, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				return 0, rpc.Errorf(rpc.Invalid, "Malformed upload data.")
			}
			r.buf = d
		}
	}
	n := copy(b, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (m *module) hWriteStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
	var p struct {
		pathParams
		Size      int64 `json:"size"`
		Overwrite bool  `json:"overwrite"`
	}
	if err := c.Bind(&p); err != nil {
		return err
	}
	pth, err := cleanRemote(p.Path)
	if err != nil {
		return err
	}
	h, err := m.pool.get(ctx, p.Conn)
	if err != nil {
		return remoteErr(err)
	}
	n, err := writeFile(ctx, h, posixOps, pth, &inputReader{ctx: ctx, s: s}, p.Size, p.Overwrite, nil)
	h.release(err)
	if err != nil {
		return remoteErr(err)
	}
	return s.Send(map[string]any{"done": true, "size": n, "path": pth})
}
