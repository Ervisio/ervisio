package sshauth

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// SSHDConfig is sshd's configuration file (variable for tests).
var SSHDConfig = "/etc/ssh/sshd_config"

// DefaultAuthorizedKeysFiles is sshd's default AuthorizedKeysFile.
var DefaultAuthorizedKeysFiles = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}

// maxKeysFile bounds an authorized_keys file.
const maxKeysFile = 1 << 20

// User is the account whose keys are checked.
type User struct {
	Name string
	UID  uint32
	GID  uint32
	Home string
	// Groups are the supplementary group ids (from NSS) used while
	// reading the files as the user.
	Groups []uint32
}

// AuthorizedKeysFiles returns the authorized_keys paths for u from the
// global (non-Match) AuthorizedKeysFile directive of sshd_config, following
// Include, with %h %u %U %% expanded and relative paths taken from the home
// directory. Directives inside Match blocks are not applied. Without the
// directive (or when the config cannot be read) sshd's default is used;
// "none" yields no files.
func AuthorizedKeysFiles(u User) []string {
	vals, _ := authorizedKeysDirective(SSHDConfig, 0)
	if vals == nil {
		vals = DefaultAuthorizedKeysFiles
	}
	var out []string
	for _, v := range vals {
		if strings.EqualFold(v, "none") {
			return nil
		}
		p, ok := expandTokens(v, u)
		if !ok {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(u.Home, p)
		}
		out = append(out, filepath.Clean(p))
	}
	return out
}

func expandTokens(v string, u User) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] != '%' {
			b.WriteByte(v[i])
			continue
		}
		if i+1 >= len(v) {
			return "", false
		}
		i++
		switch v[i] {
		case '%':
			b.WriteByte('%')
		case 'h':
			b.WriteString(u.Home)
		case 'u':
			b.WriteString(u.Name)
		case 'U':
			b.WriteString(strconv.FormatUint(uint64(u.UID), 10))
		default:
			return "", false // %k etc. are not supported
		}
	}
	return b.String(), true
}

// authorizedKeysDirective returns the first AuthorizedKeysFile values in
// file (nil when not set), reading Include files in place.
func authorizedKeysDirective(file string, depth int) ([]string, error) {
	if depth > 8 {
		return nil, errors.New("include depth")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 1<<20))
	sc.Buffer(make([]byte, 4096), 64<<10)
	for sc.Scan() {
		words := splitConfigLine(sc.Text())
		if len(words) == 0 {
			continue
		}
		switch strings.ToLower(words[0]) {
		case "match":
			return nil, nil // the rest of this file is conditional
		case "include":
			for _, pat := range words[1:] {
				if !filepath.IsAbs(pat) {
					pat = filepath.Join(filepath.Dir(SSHDConfig), pat)
				}
				matches, _ := filepath.Glob(pat)
				for _, m := range matches {
					if v, _ := authorizedKeysDirective(m, depth+1); v != nil {
						return v, nil
					}
				}
			}
		case "authorizedkeysfile":
			if len(words) > 1 {
				return words[1:], nil
			}
		}
	}
	return nil, sc.Err()
}

// splitConfigLine splits an sshd_config line into words: "key value",
// "key=value", double-quoted words, # comments.
func splitConfigLine(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return nil
	}
	if k, v, ok := strings.Cut(line, "="); ok && !strings.ContainsAny(k, " \t\"") {
		line = k + " " + v
	}
	var out []string
	var cur strings.Builder
	inQ, have := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQ = !inQ
			have = true
		case !inQ && (c == ' ' || c == '\t'):
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		case !inQ && c == '#' && !have:
			return out
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// ReadAuthorizedKeys reads the user's authorized_keys files with the
// user's file-system identity (setfsuid when the daemon is root), so a
// symlink cannot make the daemon read anything the user could not. Each
// file must pass sshd's StrictModes checks: a regular file owned by the
// user or root and not writable by group or others, and every directory
// from the file up to the home directory (or /) owned by the user or root
// and not group/world writable. Missing files are skipped; files failing
// the checks are skipped and reported in problems.
func ReadAuthorizedKeys(u User) (data []byte, problems []string) {
	files := AuthorizedKeysFiles(u)
	var buf bytes.Buffer
	err := asUser(u, func() error {
		for _, f := range files {
			b, err := readSecure(f, u)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					problems = append(problems, fmt.Sprintf("%s: %v", f, err))
				}
				continue
			}
			buf.Write(b)
			buf.WriteByte('\n')
		}
		return nil
	})
	if err != nil {
		problems = append(problems, fmt.Sprintf("cannot read the files as %s: %v", u.Name, err))
	}
	return buf.Bytes(), problems
}

func ownerOK(st *syscall.Stat_t, uid uint32) bool { return st.Uid == 0 || st.Uid == uid }

// readSecure opens path (symlinks resolved, like sshd), checks it and
// its directories, and reads it.
func readSecure(p string, u User) ([]byte, error) {
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(int(f.Fd()), &st); err != nil {
		return nil, err
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, errors.New("not a regular file")
	}
	if !ownerOK(&st, u.UID) || st.Mode&0o022 != 0 {
		return nil, fmt.Errorf("bad ownership or modes (owner uid %d, mode %04o)", st.Uid, st.Mode&0o7777)
	}
	if err := checkDirs(filepath.Dir(real), u); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, maxKeysFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxKeysFile {
		return nil, errors.New("larger than 1 MiB")
	}
	return b, nil
}

// checkDirs walks from dir up to the home directory (included) or /, as
// sshd's auth_secure_path does.
func checkDirs(dir string, u User) error {
	home, err := filepath.EvalSymlinks(u.Home)
	if err != nil {
		home = filepath.Clean(u.Home)
	}
	for {
		var st syscall.Stat_t
		if err := syscall.Stat(dir, &st); err != nil {
			return err
		}
		if !ownerOK(&st, u.UID) || st.Mode&0o022 != 0 {
			return fmt.Errorf("bad ownership or modes for directory %s (owner uid %d, mode %04o)", dir, st.Uid, st.Mode&0o7777)
		}
		if dir == home || dir == "/" {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// asUser runs fn with u's file-system identity (fsuid, fsgid and the
// user's supplementary groups) when the process is root and u is not, as
// sshd's temporarily_use_uid does. The switch happens on a dedicated, locked
// OS thread (setfsuid, setfsgid and the raw setgroups system call only
// change the calling thread): the caller's thread is never touched, and if
// the identity cannot be restored the thread is not unlocked, so the Go
// runtime discards it when the goroutine ends. setfsuid/setfsgid do not
// report failure, so the switch is read back and fn does not run unless it
// took effect.
func asUser(u User, fn func() error) error {
	if os.Geteuid() != 0 || u.UID == 0 {
		return fn()
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		restore, err := switchFS(u)
		if err == nil {
			err = fn()
		}
		if restore() == nil {
			runtime.UnlockOSThread()
		}
		done <- err
	}()
	return <-done
}

// switchFS changes the calling thread's file-system identity to u and
// returns the function that puts root's back (nil error when it did).
func switchFS(u User) (restore func() error, err error) {
	oldGroups, err := unix.Getgroups()
	if err != nil {
		return func() error { return nil }, err
	}
	restore = func() error {
		e1 := unix.Setfsuid(0)
		e2 := unix.Setfsgid(0)
		e3 := unix.Setgroups(oldGroups)
		uid, _ := unix.SetfsuidRetUid(-1)
		gid, _ := unix.SetfsgidRetGid(-1)
		if e := errors.Join(e1, e2, e3); e != nil {
			return e
		}
		if uid != 0 || gid != 0 {
			return errors.New("file-system identity not restored")
		}
		return nil
	}
	groups := make([]int, 0, len(u.Groups)+1)
	groups = append(groups, int(u.GID))
	for _, g := range u.Groups {
		if g != u.GID {
			groups = append(groups, int(g))
		}
	}
	if err := unix.Setgroups(groups); err != nil {
		return restore, fmt.Errorf("setgroups: %w", err)
	}
	if err := unix.Setfsgid(int(u.GID)); err != nil {
		return restore, err
	}
	if err := unix.Setfsuid(int(u.UID)); err != nil {
		return restore, err
	}
	uid, _ := unix.SetfsuidRetUid(-1)
	gid, _ := unix.SetfsgidRetGid(-1)
	if uid != int(u.UID) || gid != int(u.GID) {
		return restore, fmt.Errorf("cannot take the file-system identity of uid %d (now fsuid %d, fsgid %d)", u.UID, uid, gid)
	}
	return restore, nil
}
