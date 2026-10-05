//go:build unix

package files

// File system access that stays safe when the bridge runs as root.
//
// Every operation that changes something works on directory file descriptors:
// the parent directory of the target is opened once, and the target is then
// reached with *at() system calls on its bare name, never following a link.
// Recursive walks open each directory with O_NOFOLLOW|O_DIRECTORY and act on
// the opened descriptors, so another user who owns part of the tree cannot
// swap a directory for a link while the walk runs and redirect root's change
// outside the tree (docs/SECURITY-REVIEW.md, H1).
//
// In the root bridge (strictLinks) the parent directory itself is resolved
// one component at a time, and the only links followed on the way are links
// owned by root: a user's link in the middle of a path is refused. In the user
// bridge paths resolve normally: the user can only reach what they may reach.

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

var (
	// strictLinks restricts which links a path may go through (root bridge).
	strictLinks = os.Geteuid() == 0
	// trustedLinkUID owns the only links followed in strict mode. Tests change it.
	trustedLinkUID uint32 = 0
)

const maxLinkHops = 40

// raceHook lets tests swap files at the moments an attacker would: before an
// entry is looked at ("lstat"), between the look and the open ("open"), and
// after a directory was opened, before its children are read ("children").
var raceHook = func(stage, full string) {}

func isLink(st *unix.Stat_t) bool { return st.Mode&unix.S_IFMT == unix.S_IFLNK }
func isDir(st *unix.Stat_t) bool  { return st.Mode&unix.S_IFMT == unix.S_IFDIR }
func isReg(st *unix.Stat_t) bool  { return st.Mode&unix.S_IFMT == unix.S_IFREG }

func lstatAt(dirfd int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

func readlinkAt(dirfd int, name string) (string, error) {
	for size := 256; size <= 1<<16; size *= 4 {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(dirfd, name, buf)
		if err != nil {
			return "", err
		}
		if n < size {
			return string(buf[:n]), nil
		}
	}
	return "", unix.ENAMETOOLONG
}

// openDir opens path as a directory (O_RDONLY) and returns the descriptor.
func openDir(path string) (*os.File, error) {
	if !strictLinks {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, pathErr("open", path, err)
		}
		return os.NewFile(uintptr(fd), path), nil
	}
	fd, err := resolveStrict(path)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	dfd, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, pathErr("open", path, err)
	}
	return os.NewFile(uintptr(dfd), path), nil
}

// resolveStrict walks path from / one component at a time with O_PATH|O_NOFOLLOW
// and returns an O_PATH descriptor of the directory it names. Links are followed
// only when they belong to trustedLinkUID (root): the link inode checked is the
// one read, so a link cannot be swapped between the check and the use.
func resolveStrict(path string) (int, error) {
	if !filepath.IsAbs(path) {
		return -1, rpc.Errorf(rpc.Invalid, "The path %q must start with /.", path)
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, pathErr("open", "/", err)
	}
	comps := strings.Split(path, "/")
	hops := 0
	for len(comps) > 0 {
		c := comps[0]
		comps = comps[1:]
		if c == "" || c == "." {
			continue
		}
		nfd, err := unix.Openat(fd, c, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			unix.Close(fd)
			return -1, pathErr("open", path, err)
		}
		var st unix.Stat_t
		if err := unix.Fstat(nfd, &st); err != nil {
			unix.Close(nfd)
			unix.Close(fd)
			return -1, pathErr("stat", path, err)
		}
		switch {
		case isLink(&st):
			if st.Uid != trustedLinkUID {
				unix.Close(nfd)
				unix.Close(fd)
				return -1, errUntrustedLink(path)
			}
			hops++
			if hops > maxLinkHops {
				unix.Close(nfd)
				unix.Close(fd)
				return -1, pathErr("open", path, unix.ELOOP)
			}
			target, err := readlinkAt(nfd, "")
			unix.Close(nfd)
			if err != nil {
				unix.Close(fd)
				return -1, pathErr("readlink", path, err)
			}
			if strings.HasPrefix(target, "/") {
				unix.Close(fd)
				if fd, err = unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0); err != nil {
					return -1, pathErr("open", "/", err)
				}
			}
			comps = append(strings.Split(target, "/"), comps...)
		case isDir(&st):
			unix.Close(fd)
			fd = nfd
		default:
			unix.Close(nfd)
			unix.Close(fd)
			return -1, pathErr("open", path, unix.ENOTDIR)
		}
	}
	return fd, nil
}

// openParent opens the directory holding path and returns it with the last
// path component. path must be clean and absolute, and not "/".
func openParent(path string) (*os.File, string, error) {
	name := filepath.Base(path)
	if path == "/" || name == "." || name == ".." || name == "/" {
		return nil, "", rpc.Errorf(rpc.Invalid, "%q cannot be changed.", path)
	}
	d, err := openDir(filepath.Dir(path))
	if err != nil {
		return nil, "", err
	}
	return d, name, nil
}

func dfd(f *os.File) int { return int(f.Fd()) }

// openSubdir opens name below dirfd as a directory without following a link.
func openSubdir(dirfd int, name, full string) (*os.File, error) {
	fd, err := unix.Openat(dirfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, pathErr("open", full, err)
	}
	return os.NewFile(uintptr(fd), full), nil
}

// lstatPath is os.Lstat through the safe parent resolution.
func lstatPath(path string) (unix.Stat_t, error) {
	d, name, err := openParent(path)
	if err != nil {
		return unix.Stat_t{}, err
	}
	defer d.Close()
	st, err := lstatAt(dfd(d), name)
	return st, pathErr("lstat", path, err)
}

// ---- chmod ----

// unixMode turns permission and special bits into the chmod argument.
func unixMode(m os.FileMode) uint32 {
	v := uint32(m.Perm())
	if m&os.ModeSetuid != 0 {
		v |= unix.S_ISUID
	}
	if m&os.ModeSetgid != 0 {
		v |= unix.S_ISGID
	}
	if m&os.ModeSticky != 0 {
		v |= unix.S_ISVTX
	}
	return v
}

var errIsLink = errors.New("is a link")

// chmodNoFollow changes the mode of name below dirfd; links are refused with
// errIsLink. The object is pinned with an O_PATH descriptor, so a swap after the
// check changes nothing outside the tree.
func chmodNoFollow(dirfd int, name string, mode uint32) error {
	fd, err := unix.Openat(dirfd, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if isLink(&st) {
		return errIsLink
	}
	err = unix.Fchmodat(fd, "", mode, unix.AT_EMPTY_PATH)
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL) {
		// No fchmodat2 (Linux < 6.6): go through the descriptor's magic link.
		err = unix.Chmod("/proc/self/fd/"+strconv.Itoa(fd), mode)
	}
	return err
}

// walkAt visits name below dirfd and, for a directory, everything inside it,
// never following links. For a directory, self is a descriptor opened on it
// (visit runs before its children); for anything else self is -1. Errors on
// single entries go to onErr and the walk goes on.
func walkAt(ctx context.Context, dirfd int, name, full string, visit func(dirfd int, name string, st *unix.Stat_t, self int) error, onErr func(error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raceHook("lstat", full)
	st, err := lstatAt(dirfd, name)
	if err != nil {
		onErr(pathErr("lstat", full, err))
		return nil
	}
	raceHook("open", full)
	if !isDir(&st) {
		if err := visit(dirfd, name, &st, -1); err != nil {
			onErr(pathErr("change", full, err))
		}
		return nil
	}
	d, err := openSubdir(dirfd, name, full)
	if err != nil {
		onErr(err)
		return nil
	}
	defer d.Close()
	if err := unix.Fstat(dfd(d), &st); err != nil {
		onErr(pathErr("stat", full, err))
		return nil
	}
	if err := visit(dirfd, name, &st, dfd(d)); err != nil {
		onErr(pathErr("change", full, err))
	}
	raceHook("children", full)
	names, err := d.Readdirnames(-1)
	if err != nil {
		onErr(pathErr("readdir", full, err))
	}
	for _, n := range names {
		if err := walkAt(ctx, dfd(d), n, full+"/"+n, visit, onErr); err != nil {
			return err
		}
	}
	return nil
}

// ---- remove ----

// removeAllAt removes name below dirfd and everything inside it without
// following links. A missing name is not an error.
func removeAllAt(dirfd int, name, full string) error {
	err := unix.Unlinkat(dirfd, name, 0)
	if err == nil || errors.Is(err, unix.ENOENT) {
		return nil
	}
	if !errors.Is(err, unix.EISDIR) && !errors.Is(err, unix.EPERM) {
		return pathErr("unlink", full, err)
	}
	d, oerr := openSubdir(dirfd, name, full)
	if oerr != nil {
		if errors.Is(oerr, unix.ENOTDIR) || errors.Is(oerr, unix.ELOOP) {
			return pathErr("unlink", full, unix.Unlinkat(dirfd, name, 0))
		}
		return pathErr("unlink", full, err)
	}
	var first error
	names, rerr := d.Readdirnames(-1)
	if rerr != nil {
		first = pathErr("readdir", full, rerr)
	}
	for _, n := range names {
		if err := removeAllAt(dfd(d), n, full+"/"+n); err != nil && first == nil {
			first = err
		}
	}
	d.Close()
	if err := unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
		if first != nil {
			return first
		}
		return pathErr("rmdir", full, err)
	}
	return nil
}

// removePath is os.RemoveAll through the safe parent resolution.
func removePath(path string) error {
	d, name, err := openParent(path)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	defer d.Close()
	return removeAllAt(dfd(d), name, path)
}

// ---- create ----

// tempName returns a fresh name for a hidden temporary file.
func tempName(prefix string) string {
	return prefix + strconv.FormatUint(rand.Uint64(), 36)
}

// createTempAt creates a new file in dirfd with O_EXCL (which never follows a
// link) and mode 0600.
func createTempAt(dirfd int, prefix, dir string) (*os.File, string, error) {
	for i := 0; i < 100; i++ {
		n := tempName(prefix)
		fd, err := unix.Openat(dirfd, n, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", pathErr("create", filepath.Join(dir, n), err)
		}
		return os.NewFile(uintptr(fd), filepath.Join(dir, n)), n, nil
	}
	return nil, "", pathErr("create", dir, unix.EEXIST)
}

// mkdirAll is os.MkdirAll through the safe resolution: each missing level is
// created with mkdirat in the descriptor of the level above.
func mkdirAll(path string, perm uint32) error {
	if d, err := openDir(path); err == nil {
		d.Close()
		return nil
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := mkdirAll(parent, perm); err != nil {
			return err
		}
	}
	d, name, err := openParent(path)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := unix.Mkdirat(dfd(d), name, perm); err != nil {
		if errors.Is(err, unix.EEXIST) {
			if st, e := lstatAt(dfd(d), name); e == nil && isDir(&st) {
				return nil
			}
		}
		return pathErr("mkdir", path, err)
	}
	return nil
}

// ---- rename / copy ----

// renameNoReplace renames without ever replacing an existing target.
func renameNoReplace(sdir int, sname string, ddir int, dname, dfull string) error {
	err := unix.Renameat2(sdir, sname, ddir, dname, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) {
		// File systems without RENAME_NOREPLACE: check, then rename.
		if _, e := lstatAt(ddir, dname); e == nil {
			return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", dfull)
		}
		err = unix.Renameat(sdir, sname, ddir, dname)
	}
	if errors.Is(err, unix.EEXIST) {
		return rpc.Errorf(rpc.Conflict, "%s already exists. Choose another name.", dfull)
	}
	if errors.Is(err, unix.EXDEV) {
		return err
	}
	return pathErr("rename", dfull, err)
}

type devIno struct{ dev, ino uint64 }

// copier copies trees between directory descriptors.
type copier struct {
	ctx      context.Context
	progress func(n int64)
	made     map[devIno]bool // directories this copy created (never copied into themselves)
	buf      []byte
	depth    int
	madeTop  bool // the top-level target was created by this copy (so it may be removed on failure)
}

func (c *copier) created() {
	if c.depth == 1 {
		c.madeTop = true
	}
}

func newCopier(ctx context.Context, progress func(int64)) *copier {
	return &copier{ctx: ctx, progress: progress, made: map[devIno]bool{}}
}

func setTimes(dirfd int, name string, st *unix.Stat_t) error {
	ts := []unix.Timespec{unix.NsecToTimespec(time.Now().UnixNano()), st.Mtim}
	return unix.UtimesNanoAt(dirfd, name, ts, unix.AT_SYMLINK_NOFOLLOW)
}

// copyAt copies sname in sdir to dname in ddir (which must not exist), without
// following links. Sockets, devices and pipes are skipped.
func (c *copier) copyAt(sdir int, sname, sfull string, ddir int, dname, dfull string) error {
	if err := c.ctx.Err(); err != nil {
		return rpc.Errorf(rpc.Unavailable, "The copy was cancelled.")
	}
	c.depth++
	defer func() { c.depth-- }()
	raceHook("lstat", sfull)
	st, err := lstatAt(sdir, sname)
	if err != nil {
		return pathErr("lstat", sfull, err)
	}
	raceHook("open", sfull)
	switch {
	case isLink(&st):
		t, err := readlinkAt(sdir, sname)
		if err != nil {
			return pathErr("readlink", sfull, err)
		}
		if err := unix.Symlinkat(t, ddir, dname); err != nil {
			return pathErr("symlink", dfull, err)
		}
		c.created()
		return nil
	case isDir(&st):
		return c.copyDir(sdir, sname, sfull, ddir, dname, dfull)
	case isReg(&st):
		return c.copyFile(sdir, sname, sfull, ddir, dname, dfull)
	}
	return nil
}

func (c *copier) copyDir(sdir int, sname, sfull string, ddir int, dname, dfull string) error {
	src, err := openSubdir(sdir, sname, sfull)
	if err != nil {
		return err
	}
	defer src.Close()
	var st unix.Stat_t
	if err := unix.Fstat(dfd(src), &st); err != nil {
		return pathErr("stat", sfull, err)
	}
	if c.made[devIno{st.Dev, st.Ino}] {
		return nil // the copy itself, reached through the source
	}
	if err := unix.Mkdirat(ddir, dname, st.Mode&0o777|0o700); err != nil {
		return pathErr("mkdir", dfull, err)
	}
	c.created()
	dst, err := openSubdir(ddir, dname, dfull)
	if err != nil {
		return err
	}
	defer dst.Close()
	var dst0 unix.Stat_t
	if err := unix.Fstat(dfd(dst), &dst0); err != nil {
		return pathErr("stat", dfull, err)
	}
	if dst0.Uid != uint32(os.Geteuid()) {
		return rpc.Errorf(rpc.Conflict, "%s was replaced while it was being copied.", dfull)
	}
	c.made[devIno{dst0.Dev, dst0.Ino}] = true
	raceHook("children", sfull)
	names, err := src.Readdirnames(-1)
	if err != nil {
		return pathErr("readdir", sfull, err)
	}
	for _, n := range names {
		if err := c.copyAt(dfd(src), n, sfull+"/"+n, dfd(dst), n, dfull+"/"+n); err != nil {
			return err
		}
	}
	if err := unix.Fchmod(dfd(dst), st.Mode&0o777); err != nil {
		return pathErr("chmod", dfull, err)
	}
	return pathErr("chtimes", dfull, setTimes(ddir, dname, &st))
}

func (c *copier) copyFile(sdir int, sname, sfull string, ddir int, dname, dfull string) error {
	// O_NONBLOCK: a pipe swapped in after the check must not block the copy.
	ifd, err := unix.Openat(sdir, sname, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return pathErr("open", sfull, err)
	}
	defer unix.Close(ifd)
	var st unix.Stat_t
	if err := unix.Fstat(ifd, &st); err != nil {
		return pathErr("stat", sfull, err)
	}
	if !isReg(&st) {
		return nil // swapped for something that is not a file: skip, like the walk does
	}
	ofd, err := unix.Openat(ddir, dname, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return pathErr("create", dfull, err)
	}
	c.created()
	out := os.NewFile(uintptr(ofd), dfull)
	fail := func(err error) error {
		out.Close()
		_ = unix.Unlinkat(ddir, dname, 0)
		return err
	}
	if c.buf == nil {
		c.buf = make([]byte, 256<<10)
	}
	for {
		if err := c.ctx.Err(); err != nil {
			return fail(rpc.Errorf(rpc.Unavailable, "The copy was cancelled."))
		}
		n, rerr := unix.Read(ifd, c.buf)
		if n > 0 {
			if _, werr := out.Write(c.buf[:n]); werr != nil {
				return fail(werr)
			}
			if c.progress != nil {
				c.progress(int64(n))
			}
		}
		if rerr == unix.EINTR {
			continue
		}
		if rerr != nil {
			return fail(pathErr("read", sfull, rerr))
		}
		if n <= 0 {
			break
		}
	}
	if err := unix.Fchmod(ofd, st.Mode&0o777); err != nil {
		return fail(pathErr("chmod", dfull, err))
	}
	if err := out.Close(); err != nil {
		_ = unix.Unlinkat(ddir, dname, 0)
		return err
	}
	return pathErr("chtimes", dfull, setTimes(ddir, dname, &st))
}

// copyPath copies src to dst (which must not exist).
func copyPath(ctx context.Context, src, dst string, progress func(int64)) error {
	sd, sname, err := openParent(src)
	if err != nil {
		return err
	}
	defer sd.Close()
	dd, dname, err := openParent(dst)
	if err != nil {
		return err
	}
	defer dd.Close()
	c := newCopier(ctx, progress)
	if err := c.copyAt(dfd(sd), sname, src, dfd(dd), dname, dst); err != nil {
		if c.madeTop {
			_ = removeAllAt(dfd(dd), dname, dst)
		}
		return err
	}
	return nil
}

// checkExists fails unless something (a link itself, not its target) exists at path.
func checkExists(path string) error {
	_, err := lstatPath(path)
	return err
}

// mkdirOne creates the single folder path; its parent must exist.
func mkdirOne(path string, perm uint32) error {
	d, name, err := openParent(path)
	if err != nil {
		return err
	}
	err = unix.Mkdirat(dfd(d), name, perm)
	d.Close()
	return pathErr("mkdir", path, err)
}

// createFile creates the empty regular file path, failing if anything is there.
func createFile(path string, perm uint32) error {
	d, name, err := openParent(path)
	if err != nil {
		return err
	}
	fd, err := unix.Openat(dfd(d), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, perm)
	d.Close()
	if err != nil {
		return pathErr("create", path, err)
	}
	unix.Close(fd)
	return nil
}
