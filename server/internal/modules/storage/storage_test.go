package storage

import (
	"strings"
	"testing"
)

func dataDevice() Device {
	return finishDevice(Device{Device: "/dev/test-data", Name: "test-data", Type: "part", Size: 1 << 30, NativeID: "8:17", Mounts: []string{}, Filesystems: []string{"ext4", "exfat"}})
}
func formatParams(d Device) formatRequest {
	return formatRequest{Device: d.Device, Identity: d.Identity, Filesystem: "ext4", Quick: true, Confirm: d.Confirmation}
}
func TestFormatRequiresFreshIdentityAndExplicitConfirmation(t *testing.T) {
	d := dataDevice()
	p := formatParams(d)
	if err := validateFormat(d, p); err != nil {
		t.Fatal(err)
	}
	p.Identity = "old"
	if validateFormat(d, p) == nil {
		t.Fatal("stale identity accepted")
	}
	p = formatParams(d)
	p.Confirm = "yes"
	if validateFormat(d, p) == nil {
		t.Fatal("generic confirmation accepted")
	}
	changed := d
	changed.Mounts = []string{"/mnt/new"}
	changed = finishDevice(changed)
	if changed.Identity == d.Identity {
		t.Fatal("mount state was not bound to preview")
	}
}
func TestProtectedDevicesCannotBeForced(t *testing.T) {
	for _, reason := range []string{"system_volume", "active_swap", "partitioned_disk", "managed_volume", "in_use"} {
		d := dataDevice()
		d.Blocked = reason
		p := formatParams(d)
		p.Force = true
		if validateFormat(d, p) == nil {
			t.Fatal(reason + " accepted")
		}
	}
	for _, mutate := range []func(*Device){func(d *Device) { d.ReadOnly = true }, func(d *Device) { d.HasChildren = true }, func(d *Device) { d.Holders = true }, func(d *Device) { d.Size = 0 }, func(d *Device) { d.Type = "lvm" }} {
		d := dataDevice()
		mutate(&d)
		if finishDevice(d).Blocked == "" {
			t.Fatal("unsafe device is formattable")
		}
	}
}
func TestFormatOptionsAreBounded(t *testing.T) {
	d := dataDevice()
	p := formatParams(d)
	p.Filesystem = "anything"
	if validateFormat(d, p) == nil {
		t.Fatal("unknown formatter accepted")
	}
	p = formatParams(d)
	p.Label = "a/../../host"
	if validateFormat(d, p) == nil {
		t.Fatal("invalid label accepted")
	}
	p.Label = strings.Repeat("x", 17)
	if validateFormat(d, p) == nil {
		t.Fatal("oversized ext4 label accepted")
	}
	d.Mounts = []string{"/mnt/data"}
	d = finishDevice(d)
	p = formatParams(d)
	if validateFormat(d, p) == nil {
		t.Fatal("mounted volume accepted without unmount acknowledgement")
	}
	p.Force = true
	if err := validateFormat(d, p); err != nil {
		t.Fatal(err)
	}
}
func TestRevalidationStopsChangedOrStillMountedVolumes(t *testing.T) {
	before := dataDevice()
	after := before
	after.NativeID = "8:18"
	if validateSameDevice(before, after) == nil {
		t.Fatal("replacement device accepted")
	}
	after = before
	after.Mounts = []string{"/mnt/new"}
	if validateSameDevice(before, after) == nil {
		t.Fatal("mounted device accepted")
	}
}

func TestWindowsDriveLetterDoesNotRequireForce(t *testing.T) {
	d := finishDevice(Device{Device: "D:", Type: "volume", Size: 1 << 30, NativeID: "volume-id", Mounts: []string{`D:\`}, Filesystems: []string{"ntfs"}, Platform: "windows"})
	p := formatParams(d)
	p.Filesystem = "ntfs"
	if err := validateFormat(d, p); err != nil {
		t.Fatal(err)
	}
	d.Blocked = "system_or_busy_volume"
	p.Force = true
	if validateFormat(d, p) == nil {
		t.Fatal("force bypassed Windows system-volume protection")
	}
}
