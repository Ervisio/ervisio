//go:build linux

package storage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCriticalMountsAndSafeFormatterArgv(t *testing.T) {
	for _, p := range []string{"/", "/boot/efi", "/usr", "/var/lib", "/home/user", "/root", "[SWAP]"} {
		if !criticalMount(p) {
			t.Fatal("unprotected " + p)
		}
	}
	for _, p := range []string{"/mnt/data", "/media/usb", "/tmp/isolated"} {
		if criticalMount(p) {
			t.Fatal("ordinary data mount blocked " + p)
		}
	}
	tool, args, err := linuxFormatCommand(formatRequest{Filesystem: "ext4", Quick: true, Label: "data;echo X"}, "/dev/loop99")
	if err != nil || tool != "mkfs.ext4" || !reflect.DeepEqual(args, []string{"-F", "-E", "lazy_itable_init=1,lazy_journal_init=1", "-L", "data;echo X", "/dev/loop99"}) {
		t.Fatalf("argv changed: %s %q %v", tool, args, err)
	}
}
func TestFullEraseIsBoundedAndCancellable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disposable.img")
	if err := os.WriteFile(path, make([]byte, 16384), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteAt([]byte("KEEP-OUTSIDE-BOUND"), 8192); err != nil {
		t.Fatal(err)
	}
	if err = zeroDevice(context.Background(), f, 4096, func(any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes[8192:8192+18]) != "KEEP-OUTSIDE-BOUND" {
		t.Fatal("erase exceeded the requested device size")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if zeroDevice(ctx, f, 4096, func(any) error { return nil }) == nil {
		t.Fatal("cancelled erase ran")
	}
}

// This integration test is deliberately opt-in and operates ONLY on a new loop
// device attached to this test's private 64 MiB image. It never mounts the image,
// enumerates a "spare" physical device, loads kernel modules, or accepts a device
// from the environment. Ownership is revalidated before every format and detach.
func TestDisposableLoopQuickAndFullFormat(t *testing.T) {
	if os.Getenv("ERVISIO_STORAGE_LOOP_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("requires explicit ERVISIO_STORAGE_LOOP_TEST=1 and root")
	}
	for _, tool := range []string{"losetup", "lsblk", "blkid", "mkfs.ext4"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("owned-loop integration requires %s", tool)
		}
	}
	if _, err := os.Stat("/dev/loop-control"); err != nil {
		t.Skip("loop devices are unavailable; no module loading attempted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	work := t.TempDir()
	image := filepath.Join(work, "ervisio-owned-format.img")
	file, err := os.OpenFile(image, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(64 << 20); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(ctx, "losetup", "--find", "--show", "--nooverlap", "--", image).CombinedOutput()
	if err != nil {
		t.Skipf("cannot attach this owned loop image: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	loop := strings.TrimSpace(string(output))
	if !regexp.MustCompile(`^/dev/loop[0-9]+$`).MatchString(loop) {
		t.Fatalf("losetup returned an unexpected device: %q", loop)
	}
	owned := func() error {
		info, err := os.Lstat(loop)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice != 0 {
			return os.ErrInvalid
		}
		backing, err := os.ReadFile(filepath.Join("/sys/class/block", filepath.Base(loop), "loop/backing_file"))
		if err != nil {
			return err
		}
		if filepath.Clean(strings.TrimSpace(string(backing))) != image {
			return os.ErrPermission
		}
		return nil
	}
	t.Cleanup(func() {
		if err := owned(); err != nil {
			t.Errorf("refused to detach a loop whose ownership cannot be verified: %v", err)
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if out, err := exec.CommandContext(cleanupCtx, "losetup", "--detach", loop).CombinedOutput(); err != nil {
			t.Errorf("owned-loop detach failed: %v (%s)", err, out)
		}
	})
	if err := owned(); err != nil {
		t.Fatalf("new loop does not point to the exact private image: %v", err)
	}
	t.Logf("created ONLY owned test loop %s backed by %s", loop, image)
	for _, quick := range []bool{true, false} {
		// Attaching or creating a filesystem can trigger udev/blkid probes which
		// legitimately take the same advisory lock. Wait for those on our loop.
		if _, err := exec.LookPath("udevadm"); err == nil {
			if out, err := exec.CommandContext(ctx, "udevadm", "settle", "--timeout=10").CombinedOutput(); err != nil {
				t.Logf("udev settle: %v %s", err, out)
			}
		}
		lockDeadline := time.Now().Add(10 * time.Second)
		for {
			if err := owned(); err != nil {
				t.Fatal("owned-image guard before lock probe: ", err)
			}
			fd, err := unix.Open(loop, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				t.Fatal(err)
			}
			lockErr := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
			if lockErr == nil {
				_ = unix.Flock(fd, unix.LOCK_UN)
			}
			_ = unix.Close(fd)
			if lockErr == nil {
				break
			}
			if !errors.Is(lockErr, unix.EWOULDBLOCK) || time.Now().After(lockDeadline) {
				t.Fatalf("owned-loop raw flock errno: %v", lockErr)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err := owned(); err != nil {
			t.Fatal("owned-image guard before inspect: ", err)
		}
		device, err := inspect(ctx, loop)
		if err != nil {
			t.Fatal(err)
		}
		if device.Type != "loop" || len(device.Mounts) != 0 || device.Blocked != "" || device.Size != 64<<20 {
			t.Fatalf("owned loop is not an eligible, unmounted 64 MiB loop: %+v", device)
		}
		request := formatRequest{Device: loop, Identity: device.Identity, Filesystem: "ext4", Label: "ERVISIO_QA", Quick: quick, Force: false, Confirm: device.Confirmation}
		if err = validateFormat(device, request); err != nil {
			t.Fatal(err)
		}
		if err := owned(); err != nil {
			t.Fatal("owned-image guard immediately before format: ", err)
		}
		erased, formatted := false, false
		err = formatVolume(ctx, device, request, func(value any) error {
			if event, ok := value.(Event); ok {
				erased = erased || event.Phase == "erase"
				formatted = formatted || event.Phase == "format"
			}
			return nil
		})
		if err != nil {
			t.Fatalf("owned loop quick=%v: %v", quick, err)
		}
		if err := owned(); err != nil {
			t.Fatal("owned-image guard after format: ", err)
		}
		fs, err := exec.CommandContext(ctx, "blkid", "-p", "-o", "value", "-s", "TYPE", "--", loop).CombinedOutput()
		if err != nil || strings.TrimSpace(string(fs)) != "ext4" {
			t.Fatalf("owned loop filesystem verification failed: %q %v", fs, err)
		}
		if !formatted || erased == quick {
			t.Fatalf("wrong quick/full phases: quick=%v erase=%v format=%v", quick, erased, formatted)
		}
		t.Logf("verified isolated %s quick=%v as ext4; exact backing image %s", loop, quick, image)
	}
}
