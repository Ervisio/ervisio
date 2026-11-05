//go:build linux

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
	"github.com/ervisio/ervisio/server/internal/sys"
	"github.com/ervisio/ervisio/server/internal/update"
	"golang.org/x/sys/unix"
)

func openStore() (int, error) {
	if os.Geteuid() != 0 {
		return -1, errors.New("Factory reset storage requires the privileged daemon.")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(DefaultBackupDir, "/"), "/") {
		if err = unix.Mkdirat(fd, part, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			unix.Close(fd)
			return -1, err
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return -1, e
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			unix.Close(fd)
			return -1, errors.New("The reset store must have trusted root-owned parents.")
		}
	}
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Mode&0077 != 0 {
		unix.Close(fd)
		return -1, errors.New("The reset backup directory must be private (0700).")
	}
	return fd, nil
}
func writePrivate(fd int, name string, value any, exclusive bool) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp := "." + name + "-" + sys.RandHex(8)
	out, e := unix.Openat(fd, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(out), tmp)
	defer func() { f.Close(); unix.Unlinkat(fd, tmp, 0) }()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	flags := uint(0)
	if exclusive {
		flags = unix.RENAME_NOREPLACE
	}
	if err = unix.Renameat2(fd, tmp, fd, name, flags); err != nil {
		return err
	}
	return unix.Fsync(fd)
}
func readPrivate(fd int, name string, value any) error {
	in, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(in), name)
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(in, &st); err != nil {
		return err
	}
	if st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Size > 16<<20 {
		return errors.New("Unsafe factory-reset document.")
	}
	data, err := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func PrepareHelper(p *Plan, statusToken string) (*JobStatus, error) {
	if p == nil || !validJobID.MatchString(p.ID) || p.Options.BackupDir != DefaultBackupDir || len(statusToken) != 64 {
		return nil, errors.New("Invalid private reset plan.")
	}
	fd, err := openStore()
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	hash := HashToken(statusToken)
	if err = writePrivate(fd, p.ID+".plan.json", helperPlan{p, hash}, true); err != nil {
		return nil, err
	}
	state := &JobStatus{ID: p.ID, State: "running", Phase: "backup", NextURL: p.NextURL, TokenHash: hash}
	if err = writePrivate(fd, p.ID+".status.json", state, true); err != nil {
		unix.Unlinkat(fd, p.ID+".plan.json", 0)
		return nil, err
	}
	return state, nil
}
func ReadStatus(id string) (*JobStatus, error) {
	if !validJobID.MatchString(id) {
		return nil, errors.New("Invalid reset identifier.")
	}
	fd, err := openStore()
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	var state JobStatus
	if err = readPrivate(fd, id+".status.json", &state); err != nil {
		return nil, err
	}
	return &state, nil
}
func FailPrepared(id string, cause error) {
	status, err := ReadStatus(id)
	if err != nil {
		return
	}
	status.State = "failed"
	status.Phase = "failed"
	status.Error = cause.Error()
	status.FinishedAt = time.Now().UnixMilli()
	fd, err := openStore()
	if err != nil {
		return
	}
	defer unix.Close(fd)
	_ = writePrivate(fd, id+".status.json", status, false)
	_ = unix.Unlinkat(fd, id+".plan.json", 0)
}
func LaunchHelper(ctx context.Context, id string) error {
	if !validJobID.MatchString(id) {
		return errors.New("Invalid reset identifier.")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(exe, brand.LibDir+"/") && exe != brand.BinLink {
		return errors.New("Factory reset requires an installed Ervisio daemon.")
	}
	return sys.Run(ctx, "systemd-run", "--unit=ervisio-reset-"+id, "--collect", "--property=Type=exec", "--", exe, HelperFlag, id)
}

type installedService struct {
	configPath string
	updates    *update.State
}

// Also cover package work launched in a terminal or SSH, outside Software's
// transaction marker. Stopping Ervisio must not kill a package manager's PTY.
func packageProcessRunning(ctx context.Context, procDir string) (bool, error) {
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, err := strconv.ParseUint(entry.Name(), 10, 32); err != nil {
			continue
		}
		file, err := os.Open(filepath.Join(procDir, entry.Name(), "comm"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		name, err := io.ReadAll(io.LimitReader(file, 128))
		_ = file.Close()
		if err != nil {
			return false, err
		}
		switch strings.TrimSpace(string(name)) {
		case "apt", "apt-get", "aptitude", "dpkg", "dnf", "dnf5", "yum", "rpm", "pacman", "zypper", "pkcon", "flatpak", "snap":
			return true, nil
		}
	}
	return false, nil
}

func resetPackageGuard(ctx context.Context, updates *update.State) error {
	if updates.PackageTransactionRunning() {
		return errors.New("A package transaction is running. Finish it before factory reset.")
	}
	busy, err := packageProcessRunning(ctx, "/proc")
	if err != nil {
		return fmt.Errorf("Package activity cannot be checked: %w", err)
	}
	if busy {
		return errors.New("A package manager is running. Finish it before factory reset.")
	}
	return nil
}

func (s installedService) Stop(ctx context.Context) error {
	// Recheck immediately before stopping: an already-open Software stream may
	// have begun a transaction while the backup was being written.
	if err := resetPackageGuard(ctx, s.updates); err != nil {
		return err
	}
	return sys.Run(ctx, "systemctl", "stop", brand.ServiceUnit)
}
func (s installedService) Start(ctx context.Context) error {
	if err := sys.Run(ctx, "systemctl", "start", brand.ServiceUnit); err != nil {
		return err
	}
	// Type=simple only confirms exec, not readiness. Keep the staged originals
	// until the same daemon version answers using the freshly written config.
	health := update.NewHTTPHealth(s.configPath)
	defer health.Client.CloseIdleConnections()
	return health.Wait(ctx, brand.Version, 45*time.Second)
}

func RunHelper(args []string) int {
	if len(args) != 1 || !validJobID.MatchString(args[0]) || os.Geteuid() != 0 {
		return 2
	}
	id := args[0]
	fd, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer unix.Close(fd)
	lock, err := unix.Openat(fd, "reset.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return 1
	}
	defer unix.Close(lock)
	if err = unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		FailPrepared(id, errors.New("Another factory reset is running."))
		return 1
	}
	var payload helperPlan
	if err = readPrivate(fd, id+".plan.json", &payload); err != nil {
		FailPrepared(id, err)
		return 1
	}
	p := payload.Plan
	if p == nil || p.ID != id || p.Options.BackupDir != DefaultBackupDir || !planFresh(p.CreatedAt, time.Now()) {
		FailPrepared(id, errors.New("The factory-reset plan expired."))
		return 1
	}
	updates := update.DefaultState(brand.UpdatesDir)
	release, err := updates.Lock()
	if err != nil {
		FailPrepared(id, err)
		return 1
	}
	defer release()
	if updates.Running() {
		FailPrepared(id, errors.New("Finish the running Ervisio update or package transaction before factory reset."))
		return 1
	}
	guardCtx, guardCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = resetPackageGuard(guardCtx, updates)
	guardCancel()
	if err != nil {
		FailPrepared(id, err)
		return 1
	}
	status := &JobStatus{ID: id, State: "running", Phase: "backup", NextURL: p.NextURL, TokenHash: payload.TokenHash}
	rolledBack := false
	phase := func(value string) {
		status.Phase = value
		if value == "rollback" {
			rolledBack = true
		}
		_ = writePrivate(fd, id+".status.json", status, false)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	result, err := Apply(ctx, p, installedService{configPath: p.Options.ConfigPath, updates: updates}, phase)
	if result != nil {
		status.Backup = result.Backup
		status.NextURL = result.NextURL
	}
	status.FinishedAt = time.Now().UnixMilli()
	if err != nil {
		status.State = "failed"
		if rolledBack {
			status.State = "rolled-back"
		}
		status.Phase = "failed"
		status.Error = err.Error()
	} else {
		status.State = "done"
		status.Phase = "complete"
	}
	_ = writePrivate(fd, id+".status.json", status, false)
	_ = unix.Unlinkat(fd, id+".plan.json", 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
