//go:build linux

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
	"golang.org/x/sys/unix"
)

type blockDevice struct {
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	ID       string        `json:"maj:min"`
	Size     json.Number   `json:"size"`
	RO       any           `json:"ro"`
	FSType   string        `json:"fstype"`
	Label    string        `json:"label"`
	UUID     string        `json:"uuid"`
	Serial   string        `json:"serial"`
	PTType   string        `json:"pttype"`
	Mounts   []string      `json:"mountpoints"`
	Children []blockDevice `json:"children"`
}

var linuxTools = map[string]string{"ext4": "mkfs.ext4", "exfat": "mkfs.exfat", "fat32": "mkfs.vfat", "ntfs": "mkfs.ntfs"}

func inventory(ctx context.Context) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := sys.Output(ctx, "lsblk", "--json", "--bytes", "--paths", "--output", "NAME,TYPE,MAJ:MIN,SIZE,RO,FSTYPE,LABEL,UUID,SERIAL,PTTYPE,MOUNTPOINTS")
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Local volume information is unavailable: %v", err)
	}
	var doc struct {
		Devices []blockDevice `json:"blockdevices"`
	}
	if err = json.Unmarshal(output, &doc); err != nil {
		return nil, err
	}
	formats := []string{}
	for _, fs := range []string{"ext4", "exfat", "fat32", "ntfs"} {
		if _, err := sys.LookPath(linuxTools[fs]); err == nil {
			formats = append(formats, fs)
		}
	}
	swaps, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Active swap volumes cannot be checked: %v", err)
	}
	out := []Device{}
	seen := map[string]bool{}
	var add func(blockDevice)
	add = func(b blockDevice) {
		if !seen[b.Name] {
			seen[b.Name] = true
			size, _ := strconv.ParseUint(string(b.Size), 10, 64)
			mounts := []string{}
			for _, m := range b.Mounts {
				if m != "" {
					mounts = append(mounts, m)
				}
			}
			sort.Strings(mounts)
			d := Device{Device: b.Name, Name: filepath.Base(b.Name), Type: b.Type, Size: size, FSType: b.FSType, Label: b.Label, Mounts: mounts, Serial: b.Serial, UUID: b.UUID, NativeID: b.ID, ReadOnly: b.RO == true || b.RO == float64(1) || b.RO == "1", HasChildren: len(b.Children) > 0, PartitionTable: b.PTType, Filesystems: formats, Platform: "linux"}
			for _, mount := range mounts {
				if criticalMount(mount) {
					d.Blocked = "system_volume"
				}
			}
			for _, line := range strings.Split(string(swaps), "\n")[1:] {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					p, _ := filepath.EvalSymlinks(fields[0])
					q, _ := filepath.EvalSymlinks(b.Name)
					if p != "" && p == q {
						d.Blocked = "active_swap"
					}
				}
			}
			if b.ID != "" {
				holders, e := os.ReadDir(filepath.Join("/sys/dev/block", b.ID, "holders"))
				d.Holders = e != nil || len(holders) > 0
			}
			out = append(out, finishDevice(d))
		}
		for _, child := range b.Children {
			add(child)
		}
	}
	for _, d := range doc.Devices {
		add(d)
	}
	return out, nil
}
func criticalMount(m string) bool {
	if m == "/" {
		return true
	}
	for _, base := range []string{"/boot", "/usr", "/etc", "/var", "/home", "/root", "/proc", "/sys", "/dev", "/run", "/opt"} {
		if m == base || strings.HasPrefix(m, base+"/") {
			return true
		}
	}
	return m == "[SWAP]"
}
func sameDevice(d Device, name string) bool {
	if !strings.HasPrefix(name, "/dev/") {
		return false
	}
	if name == d.Device {
		return true
	}
	a, e := filepath.EvalSymlinks(name)
	if e != nil {
		return false
	}
	b, e := filepath.EvalSymlinks(d.Device)
	return e == nil && a == b
}

func formatVolume(ctx context.Context, d Device, p formatRequest, send func(any) error) error {
	fresh, err := inspect(ctx, d.Device)
	if err != nil {
		return err
	}
	if fresh.Identity != d.Identity {
		return rpc.Errorf(rpc.Conflict, "The volume changed. Inspect it again.")
	}
	canonical, err := filepath.EvalSymlinks(d.Device)
	if err != nil {
		return err
	}
	// Hold an advisory lock for the entire operation; mkfs performs its own kernel-mounted checks.
	fd, err := unix.Open(canonical, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errorText(err)
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return rpc.Errorf(rpc.Forbidden, "Only a local block device can be formatted.")
	}
	majorMinor := fmt.Sprintf("%d:%d", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev)))
	if d.NativeID != majorMinor {
		return rpc.Errorf(rpc.Conflict, "The device identity changed.")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return rpc.Errorf(rpc.Conflict, "The volume is locked by another operation.")
	}
	if len(d.Mounts) > 0 {
		if !p.Force {
			return rpc.Errorf(rpc.Conflict, "The volume is mounted.")
		}
		if err = send(Event{Phase: "unmount"}); err != nil {
			return err
		}
		mounts := append([]string{}, d.Mounts...)
		sort.Slice(mounts, func(i, j int) bool { return len(mounts[i]) > len(mounts[j]) })
		for _, mount := range mounts {
			if criticalMount(mount) {
				return rpc.Errorf(rpc.Forbidden, "System volumes cannot be unmounted or formatted.")
			}
			// No lazy/forced unmount: open handles or active processes must cause a refusal.
			if err = sys.Run(ctx, "umount", "--", mount); err != nil {
				return errorText(err)
			}
		}
	}
	after, err := inspect(ctx, d.Device)
	if err != nil {
		return err
	}
	if err = validateSameDevice(d, after); err != nil {
		return err
	}
	// O_EXCL refuses a block device mounted since the last inventory check.
	exclusive, err := unix.Open(canonical, unix.O_RDWR|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return rpc.Errorf(rpc.Conflict, "The volume is mounted or busy: %v", err)
	}
	var current unix.Stat_t
	err = unix.Fstat(exclusive, &current)
	if err != nil || current.Rdev != st.Rdev || current.Ino != st.Ino {
		unix.Close(exclusive)
		return rpc.Errorf(rpc.Conflict, "The volume changed before formatting.")
	}
	if !p.Quick {
		file := os.NewFile(uintptr(exclusive), canonical)
		err = zeroDevice(ctx, file, d.Size, send)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	} else {
		unix.Close(exclusive)
	}
	// Recheck mounts immediately before mkfs. Never pass a force-mounted flag to a formatter.
	after, err = inspect(ctx, d.Device)
	if err != nil {
		return err
	}
	if err = validateSameDevice(d, after); err != nil {
		return err
	}
	tool, args, err := linuxFormatCommand(p, canonical)
	if err != nil {
		return err
	}
	if err = send(Event{Phase: "format", Message: commandMessage(tool)}); err != nil {
		return err
	}
	if err = (sys.Cmd{Name: tool, Args: args, Timeout: -1}).Stream(ctx, func(line string) error { return send(Event{Phase: "format", Message: line}) }); err != nil {
		return errorText(err)
	}
	return nil
}

func zeroDevice(ctx context.Context, w io.WriteSeeker, size uint64, send func(any) error) error {
	if _, err := w.Seek(0, io.SeekStart); err != nil {
		return err
	}
	buffer := make([]byte, 4<<20)
	var done uint64
	last := time.Time{}
	for done < size {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := min(uint64(len(buffer)), size-done)
		written, err := w.Write(buffer[:int(n)])
		done += uint64(written)
		if err != nil {
			return err
		}
		if written != int(n) {
			return io.ErrShortWrite
		}
		if time.Since(last) > 200*time.Millisecond || done == size {
			if err = send(Event{Phase: "erase", Percent: int(done * 100 / size)}); err != nil {
				return err
			}
			last = time.Now()
		}
	}
	if syncer, ok := w.(interface{ Sync() error }); ok {
		return syncer.Sync()
	}
	return nil
}

func linuxFormatCommand(p formatRequest, device string) (string, []string, error) {
	tool := linuxTools[p.Filesystem]
	if tool == "" {
		return "", nil, errors.New("unsupported filesystem")
	}
	args := []string{}
	switch p.Filesystem {
	case "ext4":
		args = []string{"-F", "-E", "lazy_itable_init=0,lazy_journal_init=0"}
		if p.Quick {
			args = []string{"-F", "-E", "lazy_itable_init=1,lazy_journal_init=1"}
		}
		if p.Label != "" {
			args = append(args, "-L", p.Label)
		}
	case "exfat":
		if p.Label != "" {
			args = append(args, "-L", p.Label)
		}
	case "fat32":
		args = []string{"-F", "32"}
		if p.Label != "" {
			args = append(args, "-n", p.Label)
		}
	case "ntfs":
		args = []string{"-Q"}
		if p.Label != "" {
			args = append(args, "-L", p.Label)
		}
	}
	args = append(args, device)
	return tool, args, nil
}
