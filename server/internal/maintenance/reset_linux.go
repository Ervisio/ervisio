//go:build linux

package maintenance

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cfg "github.com/ervisio/ervisio/server/internal/config"
	"github.com/ervisio/ervisio/server/internal/sys"
	"golang.org/x/sys/unix"
)

const maxEntries = 200000
const maxBytes = 64 << 30

// parent pins every ancestor without following symlinks, including user-controlled .config.
func parent(path string) (*os.File, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, "", fmt.Errorf("Unsafe reset path: %s", path)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if e != nil {
			return nil, "", e
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), filepath.Dir(path)), filepath.Base(path), nil
}
func statAt(fd int, name string) (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}
func beneath(fd int, name string, flags int) (int, error) {
	return unix.Openat2(fd, name, &unix.OpenHow{Flags: uint64(flags | unix.O_NOFOLLOW | unix.O_CLOEXEC), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV})
}

type visitor func(fd int, name, rel string, st unix.Stat_t) error

func idOf(st unix.Stat_t) identity {
	return identity{st.Dev, st.Ino, st.Uid, st.Gid, st.Mode, st.Size, st.Mtim.Sec*1e9 + st.Mtim.Nsec}
}
func namesBounded(file *os.File) ([]string, error) {
	names := []string{}
	for {
		chunk, err := file.Readdirnames(256)
		names = append(names, chunk...)
		if len(names) > maxEntries {
			return nil, errors.New("A directory exceeds the reset inventory limit.")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}

func walk(ctx context.Context, fd int, name, rel string, visit visitor, count *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	*count++
	if *count > maxEntries {
		return errors.New("Too many Ervisio files to reset safely (limit 200000).")
	}
	st, err := statAt(fd, name)
	if err != nil {
		return err
	}
	kind := st.Mode & unix.S_IFMT
	if kind != unix.S_IFDIR && kind != unix.S_IFREG && kind != unix.S_IFLNK {
		return fmt.Errorf("Cannot reset special file %s; move it outside Ervisio data first.", rel)
	}
	if err = visit(fd, name, rel, st); err != nil {
		return err
	}
	if kind != unix.S_IFDIR {
		return nil
	}
	child, err := beneath(fd, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return fmt.Errorf("Cannot traverse %s without crossing a mount or link: %w", rel, err)
	}
	d := os.NewFile(uintptr(child), rel)
	defer d.Close()
	var opened unix.Stat_t
	if err = unix.Fstat(child, &opened); err != nil {
		return err
	}
	if opened.Ino != st.Ino || opened.Dev != st.Dev {
		return errors.New("Ervisio data changed while it was inspected. Preview again.")
	}
	names, err := namesBounded(d)
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, n := range names {
		if err = walk(ctx, child, n, filepath.Join(rel, n), visit, count); err != nil {
			return err
		}
	}
	return nil
}
func inspect(ctx context.Context, paths []Target, records map[string]identity) (string, []Target, int64, int64, error) {
	h := sha256.New()
	out := append([]Target(nil), paths...)
	var files, bytes int64
	count := 0
	for i, target := range out {
		out[i].Files = 0
		out[i].Bytes = 0
		p, n, err := parent(target.Path)
		if err != nil {
			return "", nil, 0, 0, err
		}
		err = walk(ctx, int(p.Fd()), n, target.Path, func(_ int, _, rel string, st unix.Stat_t) error {
			if records != nil {
				records[rel] = idOf(st)
			}
			if rel == target.Path {
				out[i].Device = st.Dev
				out[i].Inode = st.Ino
			}
			fmt.Fprintf(h, "%s\x00%d:%d:%d:%d:%d:%d:%d:%d:%d:%d\n", rel, st.Dev, st.Ino, st.Mode, st.Uid, st.Gid, st.Size, st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec)
			files++
			out[i].Files++
			if st.Mode&unix.S_IFMT == unix.S_IFREG {
				bytes += st.Size
				out[i].Bytes += st.Size
			}
			if bytes > maxBytes {
				return errors.New("Ervisio data exceeds the 64 GiB reset limit.")
			}
			return nil
		}, &count)
		_ = p.Close()
		if err != nil {
			return "", nil, 0, 0, err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), out, files, bytes, nil
}
func children(path string) ([]string, error) {
	p, n, err := parent(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer p.Close()
	fd, err := beneath(int(p.Fd()), n, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, err
	}
	d := os.NewFile(uintptr(fd), path)
	defer d.Close()
	names, err := namesBounded(d)
	sort.Strings(names)
	return names, err
}
func ownedHome(home string) bool {
	return filepath.IsAbs(home) && filepath.Clean(home) == home && home != "/" && home != "/etc" && home != "/proc" && home != "/sys" && home != "/dev" && home != "/var" && home != "/usr"
}
func homeDirs(ctx context.Context) ([]string, error) {
	output, err := sys.Output(ctx, "getent", "passwd")
	if err != nil {
		return nil, fmt.Errorf("Cannot enumerate every local/NSS Ervisio profile: %w", err)
	}
	homes := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 7 && ownedHome(fields[5]) {
			homes = append(homes, fields[5])
		}
	}
	return homes, nil
}
func osProtected() ([]string, error) {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	out := []string{}
	decode := func(s string) string {
		return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 4 {
			out = append(out, decode(f[4]))
		}
	}
	b, err = os.ReadFile("/proc/swaps")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) > 0 {
			out = append(out, decode(f[0]))
		}
	}
	return out, nil
}

var systemProtected = osProtected

func inside(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func Preview(ctx context.Context, o Options) (*Plan, error) {
	if o.ConfigPath == "" || o.StateDir == "" || o.BackupDir == "" {
		return nil, errors.New("Reset requires explicit configuration, state and backup directories.")
	}
	for _, path := range []string{o.ConfigPath, o.StateDir, o.BackupDir} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
			return nil, errors.New("Reset directories must be absolute managed paths.")
		}
	}
	if filepath.Base(filepath.Dir(o.ConfigPath)) != "ervisio" || filepath.Base(o.StateDir) != "ervisio" || (o.CacheDir != "" && filepath.Base(o.CacheDir) != "ervisio") {
		return nil, errors.New("Factory reset only accepts directories explicitly named ervisio.")
	}
	for _, managed := range []string{filepath.Dir(o.ConfigPath), o.StateDir} {
		par, leaf, e := parent(managed)
		if e != nil {
			return nil, e
		}
		var st unix.Stat_t
		e = unix.Fstat(int(par.Fd()), &st)
		if e == nil {
			entry, entryErr := statAt(int(par.Fd()), leaf)
			if entryErr == nil && (entry.Uid != uint32(os.Geteuid()) || entry.Mode&0022 != 0 || entry.Mode&unix.S_IFMT != unix.S_IFDIR) {
				e = errors.New("The managed directory itself must be a trusted private directory.")
			}
			if entryErr != nil && !errors.Is(entryErr, os.ErrNotExist) {
				e = entryErr
			}
		}
		_ = par.Close()
		if e != nil {
			return nil, e
		}
		if st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0 {
			return nil, errors.New("Managed reset directories need trusted, non-writable parents.")
		}
	}
	configurationInfo, err := os.Lstat(o.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("A regular existing Ervisio configuration is required: %w", err)
	}
	if !configurationInfo.Mode().IsRegular() {
		return nil, errors.New("The Ervisio configuration must be a regular file, not a symbolic link.")
	}
	homes := o.Homes
	if homes == nil {
		var err error
		homes, err = homeDirs(ctx)
		if err != nil {
			return nil, err
		}
	}
	o.Homes = homes
	current, _, _, err := cfg.Load(o.ConfigPath)
	if err != nil {
		return nil, err
	}
	protected, err := systemProtected()
	if err != nil {
		return nil, err
	}
	for _, path := range protected {
		if path == filepath.Dir(o.ConfigPath) || path == o.ConfigPath || path == o.StateDir {
			return nil, errors.New("The Ervisio configuration or state root is an active mount/swap. Factory reset cannot safely replace it.")
		}
	}
	protected = append(protected, filepath.Join(o.StateDir, "plugins"), filepath.Join(o.StateDir, "updates"), filepath.Join(o.StateDir, "audit"), filepath.Join(o.StateDir, ".factory-reset"))
	if o.PreserveAccess {
		protected = append(protected, filepath.Join(filepath.Dir(o.ConfigPath), "tls"))
		for _, path := range []string{current.TLS.Cert, current.TLS.Key} {
			if filepath.IsAbs(path) {
				protected = append(protected, filepath.Clean(path))
			}
		}
	}
	aiState := o.AIStateDir
	if aiState == "" {
		aiState = "/var/lib/ervisio-ai"
	}
	if !filepath.IsAbs(aiState) || filepath.Clean(aiState) != aiState || filepath.Base(aiState) != "ervisio-ai" {
		return nil, errors.New("The AI state directory must be an absolute ervisio-ai directory.")
	}
	roots := []string{filepath.Dir(o.ConfigPath), o.StateDir, aiState}
	if o.CacheDir != "" {
		roots = append(roots, o.CacheDir)
	}
	for _, home := range homes {
		if !ownedHome(home) {
			return nil, fmt.Errorf("Unsafe account home: %s", home)
		}
		roots = append(roots, filepath.Join(home, ".config/ervisio"), filepath.Join(home, ".config/linuxadmin"), filepath.Join(home, ".local/share/ervisio-ai"))
	}
	for _, root := range roots {
		if inside(o.BackupDir, root) || inside(root, o.BackupDir) {
			return nil, errors.New("Backups must be outside every directory being reset.")
		}
	}
	sort.Strings(roots)
	p := &Plan{Options: o, CreatedAt: time.Now().Unix(), NextURL: o.CurrentURL}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		return nil, err
	}
	p.ID = hex.EncodeToString(random)
	var add func(string) error
	add = func(path string) error {
		for _, skip := range protected {
			if path == skip {
				p.Preserved = append(p.Preserved, path)
				return nil
			}
		}
		par, n, e := parent(path)
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return fmt.Errorf("Cannot safely inspect %s: %w", path, e)
		}
		st, e := statAt(int(par.Fd()), n)
		_ = par.Close()
		if errors.Is(e, os.ErrNotExist) {
			return nil
		}
		if e != nil {
			return e
		}
		split := false
		for _, skip := range protected {
			if inside(skip, path) {
				split = true
			}
		}
		if split && st.Mode&unix.S_IFMT == unix.S_IFDIR {
			names, e := children(path)
			if e != nil {
				return e
			}
			for _, child := range names {
				if e = add(filepath.Join(path, child)); e != nil {
					return e
				}
			}
			return nil
		}
		for _, old := range p.Paths {
			if inside(path, old.Path) {
				return nil
			}
		}
		p.Paths = append(p.Paths, Target{Path: path})
		return nil
	}
	for _, root := range roots {
		if err = add(root); err != nil {
			return nil, err
		}
	}
	next := cfg.Default()
	if o.PreserveAccess {
		next.Listen = current.Listen
		next.TLS = current.TLS
		next.Web = current.Web
		next.Auth = current.Auth
		next.AllowRoot = current.AllowRoot
	}
	if !o.PreserveAccess {
		address, e := url.Parse(o.CurrentURL)
		if e != nil || address.Hostname() == "" {
			return nil, errors.New("The current console URL is required to preview its default address.")
		}
		p.NextURL = "https://" + net.JoinHostPort(address.Hostname(), "9090")
	}
	if err = next.Validate(); err != nil {
		return nil, err
	}
	p.Config, err = cfg.Encode(next)
	if err != nil {
		return nil, err
	}
	p.inventory = map[string]identity{}
	p.Fingerprint, p.Paths, p.Files, p.Bytes, err = inspect(ctx, p.Paths, p.inventory)
	if err != nil {
		return nil, err
	}
	sort.Strings(p.Preserved)
	return p, nil
}

type tarInfo struct {
	name string
	st   unix.Stat_t
}

func (i tarInfo) Name() string       { return i.name }
func (i tarInfo) Size() int64        { return i.st.Size }
func (i tarInfo) ModTime() time.Time { return time.Unix(i.st.Mtim.Sec, i.st.Mtim.Nsec) }
func (i tarInfo) IsDir() bool        { return i.st.Mode&unix.S_IFMT == unix.S_IFDIR }
func (i tarInfo) Sys() any           { return nil }
func (i tarInfo) Mode() os.FileMode {
	m := os.FileMode(i.st.Mode & 0777)
	if i.IsDir() {
		m |= os.ModeDir
	}
	if i.st.Mode&unix.S_IFMT == unix.S_IFLNK {
		m |= os.ModeSymlink
	}
	return m
}
func backup(ctx context.Context, p *Plan) (string, error) {
	if err := os.MkdirAll(p.Options.BackupDir, 0700); err != nil {
		return "", err
	}
	dir, _, err := parent(filepath.Join(p.Options.BackupDir, "placeholder"))
	if err != nil {
		return "", err
	}
	defer dir.Close()
	var stat unix.Stat_t
	if err = unix.Fstat(int(dir.Fd()), &stat); err != nil {
		return "", err
	}
	if stat.Mode&0077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return "", errors.New("The backup directory must be private and owned by the daemon account.")
	}
	var space unix.Statfs_t
	if err = unix.Fstatfs(int(dir.Fd()), &space); err != nil {
		return "", err
	}
	requiredSpace := uint64(p.Bytes) + uint64(p.Files)*8192 + uint64(p.Bytes)/1000 + (64 << 20)
	if uint64(space.Bavail)*uint64(space.Bsize) < requiredSpace {
		return "", errors.New("Not enough free disk space for a complete reset backup.")
	}
	name := "factory-reset-" + p.ID + ".tar.gz"
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), name)
	good := false
	defer func() {
		_ = file.Close()
		if !good {
			_ = unix.Unlinkat(int(dir.Fd()), name, 0)
		}
	}()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	count := 0
	for _, target := range p.Paths {
		par, n, e := parent(target.Path)
		if e != nil {
			return "", e
		}
		e = walk(ctx, int(par.Fd()), n, strings.TrimPrefix(target.Path, "/"), func(parentfd int, name, rel string, st unix.Stat_t) error {
			link := ""
			if st.Mode&unix.S_IFMT == unix.S_IFLNK {
				buf := make([]byte, 4096)
				n, e := unix.Readlinkat(parentfd, name, buf)
				if e != nil {
					return e
				}
				link = string(buf[:n])
			}
			header, e := tar.FileInfoHeader(tarInfo{name, st}, link)
			if e != nil {
				return e
			}
			header.Name = filepath.ToSlash(rel)
			header.Uid = int(st.Uid)
			header.Gid = int(st.Gid)
			if e = tw.WriteHeader(header); e != nil {
				return e
			}
			if st.Mode&unix.S_IFMT != unix.S_IFREG {
				return nil
			}
			rfd, e := beneath(parentfd, name, unix.O_RDONLY|unix.O_NONBLOCK)
			if e != nil {
				return e
			}
			input := os.NewFile(uintptr(rfd), rel)
			defer input.Close()
			var before, after unix.Stat_t
			if e = unix.Fstat(rfd, &before); e != nil {
				return e
			}
			if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Ino != st.Ino || before.Dev != st.Dev {
				return errors.New("A file changed during backup. Preview again.")
			}
			if _, e = io.CopyN(tw, input, st.Size); e != nil {
				return e
			}
			if e = unix.Fstat(rfd, &after); e != nil {
				return e
			}
			if before.Mtim != after.Mtim || before.Ctim != after.Ctim || before.Size != after.Size {
				return errors.New("A file changed during backup. Preview again.")
			}
			return nil
		}, &count)
		_ = par.Close()
		if e != nil {
			return "", e
		}
	}
	if err = tw.Close(); err != nil {
		return "", err
	}
	if err = gz.Close(); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = dir.Sync(); err != nil {
		return "", err
	}
	good = true
	return filepath.Join(p.Options.BackupDir, name), nil
}

func removeAt(fd int, name string) error {
	st, err := statAt(fd, name)
	if err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(fd, name, 0)
	}
	child, err := beneath(fd, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	d := os.NewFile(uintptr(child), name)
	defer d.Close()
	var opened unix.Stat_t
	if err = unix.Fstat(child, &opened); err != nil {
		return err
	}
	if opened.Ino != st.Ino || opened.Dev != st.Dev {
		return errors.New("The reset staging directory changed; no replacement was removed.")
	}
	names, err := namesBounded(d)
	if err != nil {
		return err
	}
	for _, n := range names {
		if err = removeAt(child, n); err != nil {
			return err
		}
	}
	return unix.Unlinkat(fd, name, unix.AT_REMOVEDIR)
}

// Freeze owned directories in a pinned quarantine before cleanup. A process
// holding an old directory descriptor cannot insert unrelated files after this.
// Restore metadata on rollback; unknown/replaced entries are never modified.
func securedTree(ctx context.Context, fd int, name, logical, mode string, records map[string]identity, count *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	*count++
	if *count > maxEntries {
		return errors.New("Reset staging exceeded the verified entry limit.")
	}
	expected, ok := records[logical]
	if !ok {
		return fmt.Errorf("Unbacked data appeared in reset staging: %s", logical)
	}
	st, err := statAt(fd, name)
	if err != nil {
		return err
	}
	if st.Dev != expected.Device || st.Ino != expected.Inode || st.Mode&unix.S_IFMT != expected.Mode&unix.S_IFMT {
		return fmt.Errorf("Reset data was replaced: %s", logical)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		if mode != "restore" && (st.Size != expected.Size || st.Mtim.Sec*1e9+st.Mtim.Nsec != expected.Modified) {
			return fmt.Errorf("Reset data changed: %s", logical)
		}
		return nil
	}
	child, err := beneath(fd, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	d := os.NewFile(uintptr(child), logical)
	defer d.Close()
	var opened unix.Stat_t
	if err = unix.Fstat(child, &opened); err != nil {
		return err
	}
	if opened.Ino != expected.Inode || opened.Dev != expected.Device {
		return errors.New("Reset staging directory changed while opening.")
	}
	if mode == "freeze" {
		if err = unix.Fchown(child, os.Geteuid(), os.Getegid()); err != nil {
			return err
		}
		if err = unix.Fchmod(child, 0700); err != nil {
			return err
		}
	}
	names, err := namesBounded(d)
	if err != nil {
		return err
	}
	failures := []error{}
	for _, n := range names {
		if e := securedTree(ctx, child, n, filepath.Join(logical, n), mode, records, count); e != nil {
			if mode != "restore" {
				return e
			}
			failures = append(failures, e)
		}
	}
	if mode == "restore" {
		if e := unix.Fchown(child, int(expected.UID), int(expected.GID)); e != nil {
			failures = append(failures, e)
		}
		if e := unix.Fchmod(child, expected.Mode&07777); e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}
func sameEntry(fd int, name string, expected unix.Stat_t) error {
	actual, err := statAt(fd, name)
	if err != nil {
		return err
	}
	if actual.Dev != expected.Dev || actual.Ino != expected.Ino {
		return errors.New("The reset staging name was replaced; replacement data was preserved.")
	}
	return nil
}
func Apply(ctx context.Context, p *Plan, service Service, phase func(string)) (result *Result, failure error) {
	if p == nil || service == nil {
		return nil, errors.New("A reset plan and service controller are required.")
	}
	if phase == nil {
		phase = func(string) {}
	}
	fresh, err := Preview(ctx, p.Options)
	if err != nil {
		return nil, err
	}
	if fresh.Fingerprint != p.Fingerprint || string(fresh.Config) != string(p.Config) {
		return nil, errors.New("Ervisio data or configuration changed. Create a fresh preview.")
	}
	phase("backup")
	archive, err := backup(ctx, p)
	if err != nil {
		return nil, err
	}
	result = &Result{Backup: archive, NextURL: p.NextURL}
	phase("stopping")
	if err = service.Stop(ctx); err != nil {
		return result, errors.Join(err, service.Start(context.WithoutCancel(ctx)))
	}
	type staged struct {
		parent, quarantine       *os.File
		name, temporary, logical string
		original, container      unix.Stat_t
	}
	stages := []staged{}
	successful := false
	markerCreated := false
	startAttempted := false
	defer func() {
		for _, stage := range stages {
			_ = stage.parent.Close()
			_ = stage.quarantine.Close()
		}
	}()
	defer func() {
		if !successful {
			phase("rollback")
			recoveryErrors := []error{}
			if startAttempted {
				if e := service.Stop(context.WithoutCancel(ctx)); e != nil {
					failure = errors.Join(failure, fmt.Errorf("Cannot stop the new daemon for safe rollback; protected backups and staged data were retained: %w", e))
					return
				}
			}
			if markerCreated {
				if par, name, e := parent(filepath.Join(p.Options.StateDir, ".factory-reset")); e == nil {
					if e = unix.Unlinkat(int(par.Fd()), name, 0); e != nil && !errors.Is(e, os.ErrNotExist) {
						recoveryErrors = append(recoveryErrors, e)
					}
					_ = par.Close()
				}
			}
			for i := len(stages) - 1; i >= 0; i-- {
				stage := stages[i]
				if e := sameEntry(int(stage.quarantine.Fd()), "data", stage.original); e != nil {
					recoveryErrors = append(recoveryErrors, e)
					continue
				}
				count := 0
				if e := securedTree(context.WithoutCancel(ctx), int(stage.quarantine.Fd()), "data", stage.logical, "restore", fresh.inventory, &count); e != nil {
					recoveryErrors = append(recoveryErrors, e)
				}
				if _, e := statAt(int(stage.parent.Fd()), stage.name); e == nil {
					// Preserve concurrent/new-daemon data instead of deleting anything
					// that was not in the reviewed backup.
					if e = unix.Renameat2(int(stage.parent.Fd()), stage.name, int(stage.quarantine.Fd()), "during-reset", unix.RENAME_NOREPLACE); e != nil {
						recoveryErrors = append(recoveryErrors, e)
						continue
					}
				}
				if e := unix.Renameat2(int(stage.quarantine.Fd()), "data", int(stage.parent.Fd()), stage.name, unix.RENAME_NOREPLACE); e != nil {
					recoveryErrors = append(recoveryErrors, e)
				}
			}
			if e := service.Start(context.WithoutCancel(ctx)); e != nil {
				recoveryErrors = append(recoveryErrors, e)
			}
			if len(recoveryErrors) > 0 {
				failure = errors.Join(failure, fmt.Errorf("Automatic recovery was incomplete; the protected backup is retained: %w", errors.Join(recoveryErrors...)))
			}
		}
	}()
	phase("validating")
	fingerprint, _, _, _, err := inspect(ctx, p.Paths, nil)
	if err != nil {
		return result, err
	}
	if fingerprint != p.Fingerprint {
		return result, errors.New("Ervisio data changed during backup. It was not reset; create a fresh preview.")
	}
	phase("resetting")
	for i, target := range p.Paths {
		par, name, e := parent(target.Path)
		if e != nil {
			return result, e
		}
		temporary := fmt.Sprintf(".ervisio-reset-%s-%d", p.ID, i)
		if e = unix.Mkdirat(int(par.Fd()), temporary, 0700); e != nil {
			_ = par.Close()
			return result, e
		}
		qfd, e := beneath(int(par.Fd()), temporary, unix.O_RDONLY|unix.O_DIRECTORY)
		if e != nil {
			_ = par.Close()
			return result, e
		}
		quarantine := os.NewFile(uintptr(qfd), temporary)
		var container unix.Stat_t
		if e = unix.Fstat(qfd, &container); e != nil {
			_ = quarantine.Close()
			_ = par.Close()
			return result, e
		}
		if container.Uid != uint32(os.Geteuid()) || container.Mode&0077 != 0 {
			_ = quarantine.Close()
			_ = par.Close()
			return result, errors.New("The private reset quarantine was replaced before it could be pinned. No source data was moved.")
		}
		e = unix.Renameat2(int(par.Fd()), name, qfd, "data", unix.RENAME_NOREPLACE)
		if e != nil {
			_ = quarantine.Close()
			_ = par.Close()
			return result, e
		}
		stages = append(stages, staged{par, quarantine, name, temporary, target.Path, unix.Stat_t{Dev: target.Device, Ino: target.Inode}, container})
		original, e := statAt(qfd, "data")
		if e != nil {
			return result, e
		}
		stages[len(stages)-1].original = original
		if original.Dev != target.Device || original.Ino != target.Inode {
			return result, errors.New("The reviewed source was replaced while staging. No replacement was deleted.")
		}
		count := 0
		if e = securedTree(ctx, qfd, "data", target.Path, "freeze", fresh.inventory, &count); e != nil {
			return result, e
		}
	}
	if err = os.MkdirAll(filepath.Dir(p.Options.ConfigPath), 0700); err != nil {
		return result, err
	}
	if err = cfg.WriteAtomic(p.Options.ConfigPath, p.Config, 0600); err != nil {
		return result, err
	}
	// A non-secret operational marker blocks old global LinuxAdmin import.
	if err = os.MkdirAll(p.Options.StateDir, 0700); err != nil {
		return result, err
	}
	marker := filepath.Join(p.Options.StateDir, ".factory-reset")
	if _, e := os.Lstat(marker); errors.Is(e, os.ErrNotExist) {
		if err = cfg.WriteAtomic(marker, []byte("Ervisio factory reset completed\n"), 0600); err != nil {
			return result, err
		}
		markerCreated = true
	}
	phase("restarting")
	startAttempted = true
	if err = service.Start(ctx); err != nil {
		return result, err
	}
	successful = true
	for _, stage := range stages {
		count := 0
		if err = sameEntry(int(stage.quarantine.Fd()), "data", stage.original); err != nil {
			return result, err
		}
		if err = securedTree(ctx, int(stage.quarantine.Fd()), "data", stage.logical, "verify", fresh.inventory, &count); err != nil {
			return result, fmt.Errorf("Reset applied; protected staging was retained because it changed: %w", err)
		}
	}
	for _, stage := range stages {
		if err = removeAt(int(stage.quarantine.Fd()), "data"); err != nil {
			return result, fmt.Errorf("Reset succeeded, but old protected data cleanup failed: %w", err)
		}
		if err = sameEntry(int(stage.parent.Fd()), stage.temporary, stage.container); err != nil {
			return result, err
		}
		if err = unix.Unlinkat(int(stage.parent.Fd()), stage.temporary, unix.AT_REMOVEDIR); err != nil {
			return result, err
		}
	}
	phase("complete")
	return result, nil
}
