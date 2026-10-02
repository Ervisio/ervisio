package legacy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/brand"
)

// Systemd is the part of systemctl the migration uses.
type Systemd interface {
	// Systemctl runs `systemctl args...`.
	Systemctl(ctx context.Context, args ...string) error
	IsActive(ctx context.Context, unit string) bool
	IsEnabled(ctx context.Context, unit string) bool
}

// Systemctl is the real systemd.
type Systemctl struct{}

func systemctlCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	cmd.Dir = "/"
	return cmd
}

// Systemctl implements Systemd.
func (Systemctl) Systemctl(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := systemctlCmd(ctx, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsActive implements Systemd.
func (Systemctl) IsActive(ctx context.Context, unit string) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return systemctlCmd(ctx, "is-active", "--quiet", unit).Run() == nil
}

// IsEnabled implements Systemd.
func (Systemctl) IsEnabled(ctx context.Context, unit string) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return systemctlCmd(ctx, "is-enabled", "--quiet", unit).Run() == nil
}

// MigrateSchedule moves the nightly update timer of Software › Update all
// (linuxadmin-update.timer and .service, written by LinuxAdmin) to
// ervisio-update.*, keeping its time. Units an administrator wrote under
// those names are left alone.
func MigrateSchedule(ctx context.Context, p Paths, sd Systemd, logf func(string, ...any)) error {
	oldT, oldS := p.At(systemdDir+"/"+legacyTimer), p.At(systemdDir+"/"+legacyTimerSvc)
	newT, newS := p.At(systemdDir+"/"+newTimer), p.At(systemdDir+"/"+newTimerSvc)
	t, err := os.ReadFile(oldT)
	if err != nil {
		return nil
	}
	s, err := os.ReadFile(oldS)
	if err != nil {
		return nil
	}
	written := "# Written by " + brand.LegacyName
	if !strings.HasPrefix(string(t), written) || !strings.HasPrefix(string(s), written) {
		return nil
	}
	rename := func(b []byte) []byte {
		lines := strings.SplitAfter(string(b), "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "#") || strings.HasPrefix(l, "Description=") {
				lines[i] = strings.ReplaceAll(l, brand.LegacyName, brand.Name)
			}
		}
		return []byte(strings.Join(lines, ""))
	}
	if !exists(newT) {
		if err := writeFileAtomic(newS, rename(s), 0o644); err != nil {
			return err
		}
		if err := writeFileAtomic(newT, rename(t), 0o644); err != nil {
			return err
		}
	}
	_ = sd.Systemctl(ctx, "disable", "--now", legacyTimer)
	os.Remove(oldT)
	os.Remove(oldS)
	if err := sd.Systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := sd.Systemctl(ctx, "enable", "--now", newTimer); err != nil {
		return err
	}
	if logf != nil {
		logf("moved the scheduled update to %s", newTimer)
	}
	return nil
}

// InstallUnit writes /etc/systemd/system/ervisio.service from the unit file
// of a release folder (packaging/ervisio.service), with install.sh's
// header. It reports whether the file was created or changed.
func InstallUnit(p Paths, releaseDir string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(releaseDir, "packaging", brand.ServiceUnit))
	if err != nil {
		return false, fmt.Errorf("the release has no packaging/%s: %w", brand.ServiceUnit, err)
	}
	want := append([]byte(InstallerUnitHeader+"\n"), b...)
	dst := p.At(newUnitFile)
	if cur, err := os.ReadFile(dst); err == nil && string(cur) == string(want) {
		return false, nil
	}
	return true, writeFileAtomic(dst, want, 0o644)
}
