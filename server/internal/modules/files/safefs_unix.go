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
	fd, err := openRoot()
	if err != nil {
		return -1, err
	}
	comps := strings.Split(path, "/")
	hops := 0
	for len(comps) > 0 {
		c := comps[0]
		comps = comps[1:]
		if c == "" || c == "." {
			continue
		}
		nfd, st, err := openComponent(fd, c, path)
		if err != nil {
			unix.Close(fd)
			return -1, err
		}
		switch {
		case isLink(&st):
			hops++
			target, err := readTrustedLink(nfd, &st, path, hops)
			if err != nil {
				unix.Close(fd)
				return -1, err
			}
			if strings.HasPrefix(target, "/") {
				unix.Close(fd)
				if fd, err = openRoot(); err != nil {
					return -1, err
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

func openRoot() (int, error) {
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, pathErr("open", "/", err)
	}
	return fd, nil
}

// openComponent opens name below dirfd without following a link and stats it.
// dirfd stays open for the caller.
func openComponent(dirfd int, name, path string) (int, unix.Stat_t, error) {
	var st unix.Stat_t
	fd, err := unix.Openat(dirfd, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, st, pathErr("open", path, err)
	}
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return -1, st, pathErr("stat", path, err)
	}
	return fd, st, nil
}

// readTrustedLink reads the link on fd (always closing fd) after checking that
// it belongs to the trusted user and that hops is still within the limit.
func readTrustedLink(fd int, st *unix.Stat_t, path string, hops int) (string, error) {
	defer unix.Close(fd)
	if st.Uid != trustedLinkUID {
		return "", errUntrustedLink(path)
	}
	if hops > maxLinkHops {
		return "", pathErr("open", path, unix.ELOOP)
	}
	target, err := readlinkAt(fd, "")
	if err != nil {
		return "", pathErr("readlink", path, err)
	}
	return target, nil
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

// entryRef names an entry by the directory it lies in (a descriptor), its name
// there, and its full path for error messages.
type entryRef struct {
	dirfd int
	name  string
	full  string
}

// child names an entry inside the directory open on dirfd that e refers to.
func (e entryRef) child(dirfd int, name string) entryRef {
	return entryRef{dirfd, name, e.full + "/" + name}
}

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

// visitFunc is called by walkAt for each entry; see walkAt for self.
type visitFunc func(dirfd int, name string, st *unix.Stat_t, self int) error

// walkAt visits e and, for a directory, everything inside it, never following
// links. For a directory, self is a descriptor opened on it (visit runs before
// its children); for anything else self is -1. Errors on single entries go to
// onErr and the walk goes on.
func walkAt(ctx context.Context, e entryRef, visit visitFunc, onErr func(error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raceHook("lstat", e.full)
	st, err := lstatAt(e.dirfd, e.name)
	if err != nil {
		onErr(pathErr("lstat", e.full, err))
		return nil
	}
	raceHook("open", e.full)
	if !isDir(&st) {
		if err := visit(e.dirfd, e.name, &st, -1); err != nil {
			onErr(pathErr("change", e.full, err))
		}
		return nil
	}
	return walkDir(ctx, e, visit, onErr)
}

func walkDir(ctx context.Context, e entryRef, visit visitFunc, onErr func(error)) error {
	d, err := openSubdir(e.dirfd, e.name, e.full)
	if err != nil {
		onErr(err)
		return nil
	}
	defer d.Close()
	var st unix.Stat_t
	if err := unix.Fstat(dfd(d), &st); err != nil {
		onErr(pathErr("stat", e.full, err))
		return nil
	}
	if err := visit(e.dirfd, e.name, &st, dfd(d)); err != nil {
		onErr(pathErr("change", e.full, err))
	}
	raceHook("children", e.full)
	names, err := d.Readdirnames(-1)
	if err != nil {
		onErr(pathErr("readdir", e.full, err))
	}
	for _, n := range names {
		if err := walkAt(ctx, e.child(dfd(d), n), visit, onErr); err != nil {
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
	first := removeChildren(d, full)
	d.Close()
	return removeEmptyDir(dirfd, name, full, first)
}

// removeChildren removes everything inside d and returns the first error.
func removeChildren(d *os.File, full string) error {
	var first firstErr
	names, err := d.Readdirnames(-1)
	if err != nil {
		first.note(pathErr("readdir", full, err))
	}
	for _, n := range names {
		if err := removeAllAt(dfd(d), n, full+"/"+n); err != nil {
			first.note(err)
		}
	}
	return first.err
}

// removeEmptyDir removes the directory name; first is the error of emptying it,
// reported only when the removal itself fails.
func removeEmptyDir(dirfd int, name, full string, first error) error {
	err := unix.Unlinkat(dirfd, name, unix.AT_REMOVEDIR)
	if err == nil || errors.Is(err, unix.ENOENT) {
		return nil
	}
	if first != nil {
		return first
	}
	return pathErr("rmdir", full, err)
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
			return errAlreadyExists(dfull)
		}
		err = unix.Renameat(sdir, sname, ddir, dname)
	}
	if errors.Is(err, unix.EEXIST) {
		return errAlreadyExists(dfull)
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
	copyBuffer
	depth   int
	madeTop bool // the top-level target was created by this copy (so it may be removed on failure)
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

// copyAt copies src to dst (which must not exist), without following links.
// Sockets, devices and pipes are skipped.
func (c *copier) copyAt(src, dst entryRef) error {
	if err := c.ctx.Err(); err != nil {
		return errCopyCancelled()
	}
	c.depth++
	defer func() { c.depth-- }()
	raceHook("lstat", src.full)
	st, err := lstatAt(src.dirfd, src.name)
	if err != nil {
		return pathErr("lstat", src.full, err)
	}
	raceHook("open", src.full)
	switch {
	case isLink(&st):
		return c.copyLink(src, dst)
	case isDir(&st):
		return c.copyDir(src, dst)
	case isReg(&st):
		return c.copyFile(src, dst)
	}
	return nil
}

func (c *copier) copyLink(src, dst entryRef) error {
	t, err := readlinkAt(src.dirfd, src.name)
	if err != nil {
		return pathErr("readlink", src.full, err)
	}
	if err := unix.Symlinkat(t, dst.dirfd, dst.name); err != nil {
		return pathErr("symlink", dst.full, err)
	}
	c.created()
	return nil
}

func (c *copier) copyDir(src, dst entryRef) error {
	sf, err := openSubdir(src.dirfd, src.name, src.full)
	if err != nil {
		return err
	}
	defer sf.Close()
	var st unix.Stat_t
	if err := unix.Fstat(dfd(sf), &st); err != nil {
		return pathErr("stat", src.full, err)
	}
	if c.made[devIno{st.Dev, st.Ino}] {
		return nil // the copy itself, reached through the source
	}
	df, err := c.makeDir(dst, st.Mode&0o777|0o700)
	if err != nil {
		return err
	}
	defer df.Close()
	if err := c.copyChildren(sf, df, src, dst); err != nil {
		return err
	}
	if err := unix.Fchmod(dfd(df), st.Mode&0o777); err != nil {
		return pathErr("chmod", dst.full, err)
	}
	return pathErr("chtimes", dst.full, setTimes(dst.dirfd, dst.name, &st))
}

// makeDir creates the directory dst and returns it opened, after checking it
// is ours.
func (c *copier) makeDir(dst entryRef, mode uint32) (*os.File, error) {
	if err := unix.Mkdirat(dst.dirfd, dst.name, mode); err != nil {
		return nil, pathErr("mkdir", dst.full, err)
	}
	c.created()
	df, err := openSubdir(dst.dirfd, dst.name, dst.full)
	if err != nil {
		return nil, err
	}
	var st unix.Stat_t
	if err := unix.Fstat(dfd(df), &st); err != nil {
		df.Close()
		return nil, pathErr("stat", dst.full, err)
	}
	if st.Uid != uint32(os.Geteuid()) {
		df.Close()
		return nil, rpc.Errorf(rpc.Conflict, "%s was replaced while it was being copied.", dst.full)
	}
	c.made[devIno{st.Dev, st.Ino}] = true
	return df, nil
}

// copyChildren copies everything inside the open directory sf into df.
func (c *copier) copyChildren(sf, df *os.File, src, dst entryRef) error {
	raceHook("children", src.full)
	names, err := sf.Readdirnames(-1)
	if err != nil {
		return pathErr("readdir", src.full, err)
	}
	for _, n := range names {
		if err := c.copyAt(src.child(dfd(sf), n), dst.child(dfd(df), n)); err != nil {
			return err
		}
	}
	return nil
}

func (c *copier) copyFile(src, dst entryRef) error {
	// O_NONBLOCK: a pipe swapped in after the check must not block the copy.
	ifd, err := unix.Openat(src.dirfd, src.name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return pathErr("open", src.full, err)
	}
	defer unix.Close(ifd)
	var st unix.Stat_t
	if err := unix.Fstat(ifd, &st); err != nil {
		return pathErr("stat", src.full, err)
	}
	if !isReg(&st) {
		return nil // swapped for something that is not a file: skip, like the walk does
	}
	ofd, err := unix.Openat(dst.dirfd, dst.name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return pathErr("create", dst.full, err)
	}
	c.created()
	out := os.NewFile(uintptr(ofd), dst.full)
	fail := func(err error) error {
		out.Close()
		_ = unix.Unlinkat(dst.dirfd, dst.name, 0)
		return err
	}
	if err := c.pump(ifd, out, src.full); err != nil {
		return fail(err)
	}
	if err := unix.Fchmod(ofd, st.Mode&0o777); err != nil {
		return fail(pathErr("chmod", dst.full, err))
	}
	if err := out.Close(); err != nil {
		_ = unix.Unlinkat(dst.dirfd, dst.name, 0)
		return err
	}
	return pathErr("chtimes", dst.full, setTimes(dst.dirfd, dst.name, &st))
}

// pump copies everything readable from ifd into out, reporting progress.
func (c *copier) pump(ifd int, out *os.File, sfull string) error {
	buf := c.buffer()
	for {
		if err := c.ctx.Err(); err != nil {
			return errCopyCancelled()
		}
		n, rerr := unix.Read(ifd, buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			if c.progress != nil {
				c.progress(int64(n))
			}
		}
		if rerr == unix.EINTR {
			continue
		}
		if rerr != nil {
			return pathErr("read", sfull, rerr)
		}
		if n <= 0 {
			return nil
		}
	}
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
	if err := c.copyAt(entryRef{dfd(sd), sname, src}, entryRef{dfd(dd), dname, dst}); err != nil {
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
