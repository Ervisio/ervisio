// Package storage exposes explicitly confirmed formatting of non-system local volumes.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

type Device struct {
	Device         string   `json:"device"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Size           uint64   `json:"size"`
	FSType         string   `json:"fstype"`
	Label          string   `json:"label"`
	Mounts         []string `json:"mounts"`
	ReadOnly       bool     `json:"readOnly"`
	Blocked        string   `json:"blocked,omitempty"`
	Identity       string   `json:"identity"`
	Confirmation   string   `json:"confirmation"`
	Filesystems    []string `json:"filesystems"`
	Platform       string   `json:"platform"`
	Serial         string   `json:"-"`
	UUID           string   `json:"-"`
	NativeID       string   `json:"-"`
	HasChildren    bool     `json:"-"`
	Holders        bool     `json:"-"`
	PartitionTable string   `json:"-"`
}

type formatRequest struct {
	Device     string `json:"device"`
	Identity   string `json:"identity"`
	Filesystem string `json:"filesystem"`
	Label      string `json:"label"`
	Quick      bool   `json:"quick"`
	Force      bool   `json:"force"`
	Confirm    string `json:"confirm"`
}

type Event struct {
	Phase   string `json:"phase"`
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message,omitempty"`
	Device  string `json:"device,omitempty"`
}

var operation sync.Mutex

func Register(r *rpc.Registry) {
	r.Handle("storage.list", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		devices, err := inventory(ctx)
		return map[string]any{"devices": devices}, err
	})
	r.Handle("storage.inspect", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Device string `json:"device"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		return inspect(ctx, p.Device)
	})
	r.Stream("storage.format", rpc.Admin, func(ctx context.Context, c *rpc.Call, s rpc.Stream) error {
		var p formatRequest
		if err := c.Bind(&p); err != nil {
			return err
		}
		if !operation.TryLock() {
			return rpc.Errorf(rpc.Conflict, "A volume format is already running.")
		}
		defer operation.Unlock()
		ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
		defer cancel()
		d, err := inspect(ctx, p.Device)
		if err != nil {
			return err
		}
		if err := validateFormat(d, p); err != nil {
			return err
		}
		if err := s.Send(Event{Phase: "check", Device: d.Device}); err != nil {
			return err
		}
		if err := formatVolume(ctx, d, p, s.Send); err != nil {
			return err
		}
		return s.Send(Event{Phase: "done", Percent: 100, Device: d.Device})
	})
}

func inspect(ctx context.Context, name string) (Device, error) {
	if name == "" || len(name) > 1024 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Device{}, rpc.Errorf(rpc.Invalid, "Choose a local device.")
	}
	devices, err := inventory(ctx)
	if err != nil {
		return Device{}, err
	}
	for _, d := range devices {
		if sameDevice(d, name) {
			return d, nil
		}
	}
	return Device{}, rpc.Errorf(rpc.NotFound, "The selected local volume is no longer present.")
}

func finishDevice(d Device) Device {
	if d.Mounts == nil {
		d.Mounts = []string{}
	}
	if d.Filesystems == nil {
		d.Filesystems = []string{}
	}
	if d.Blocked == "" {
		switch {
		case d.ReadOnly:
			d.Blocked = "read_only"
		case d.Size == 0:
			d.Blocked = "no_media"
		case d.HasChildren || ((d.Type == "disk" || d.Type == "loop") && d.PartitionTable != ""):
			d.Blocked = "partitioned_disk"
		case d.Holders:
			d.Blocked = "in_use"
		case d.Type != "part" && d.Type != "disk" && d.Type != "loop" && d.Type != "volume":
			d.Blocked = "managed_volume"
		case len(d.Filesystems) == 0:
			d.Blocked = "tools_missing"
		}
	}
	d.Identity = deviceIdentity(d)
	d.Confirmation = "FORMAT " + d.Device
	return d
}

func deviceIdentity(d Device) string {
	// Excludes translated labels and tool availability, includes current mounts and hardware identity.
	b, _ := json.Marshal([]any{d.Device, d.NativeID, d.Serial, d.UUID, d.Size, d.Type, d.FSType, d.Label, d.Mounts, d.ReadOnly, d.HasChildren, d.Holders, d.PartitionTable, d.Blocked})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validateFormat(d Device, p formatRequest) error {
	if d.Blocked != "" {
		return rpc.Errorf(rpc.Forbidden, "This volume cannot be formatted (%s).", d.Blocked)
	}
	if p.Identity == "" || p.Identity != d.Identity {
		return rpc.Errorf(rpc.Conflict, "The volume changed since the format dialog opened. Inspect it again.")
	}
	if p.Confirm != d.Confirmation {
		return rpc.Errorf(rpc.Invalid, "Type %q to confirm erasing this volume.", d.Confirmation)
	}
	supported := false
	for _, fs := range d.Filesystems {
		if p.Filesystem == fs {
			supported = true
		}
	}
	if !supported {
		return rpc.Errorf(rpc.Invalid, "The selected filesystem is not available on this host.")
	}
	if strings.IndexFunc(p.Label, func(r rune) bool { return unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) }) >= 0 || !utf8.ValidString(p.Label) {
		return rpc.Errorf(rpc.Invalid, "The volume label contains unsupported characters.")
	}
	limit := 32
	switch p.Filesystem {
	case "ext4":
		limit = 16
	case "fat32":
		limit = 11
	case "exfat":
		limit = 15
	}
	if len([]byte(p.Label)) > limit {
		return rpc.Errorf(rpc.Invalid, "The volume label must be at most %d UTF-8 bytes.", limit)
	}
	if d.Platform != "windows" && len(d.Mounts) > 0 && !p.Force {
		return rpc.Errorf(rpc.Conflict, "The volume is mounted. Choose the unmount option before formatting it.")
	}
	return nil
}

func validateSameDevice(before, after Device) error {
	if before.Device != after.Device || before.NativeID != after.NativeID || before.Serial != after.Serial || before.Size != after.Size || after.Blocked != "" || len(after.Mounts) > 0 {
		return rpc.Errorf(rpc.Conflict, "The volume changed or is still in use. Formatting was stopped.")
	}
	return nil
}

func errorText(err error) error         { return rpc.Errorf(rpc.Unavailable, "Formatting failed: %v", err) }
func commandMessage(tool string) string { return fmt.Sprintf("Creating the filesystem with %s", tool) }
