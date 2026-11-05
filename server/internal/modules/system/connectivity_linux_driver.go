package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Kept platform-neutral so the Linux backend can be tested with a fake sysfs
// tree and fake command runner on every CI platform.
type linuxConnectivity struct {
	root     string
	lookPath func(string) (string, error)
	run      radioRunner
}

var controllerLine = regexp.MustCompile(`(?m)^Controller ([0-9A-Fa-f:]{17}) (.+?)(?: \[default\])?\s*$`)
var hciName = regexp.MustCompile(`^hci[0-9]+$`)

func (d *linuxConnectivity) devices(kind string) []RadioDevice {
	out := []RadioDevice{}
	path := filepath.Join(d.root, "sys/class/net")
	if kind == "bluetooth" {
		path = filepath.Join(d.root, "sys/class/bluetooth")
	}
	entries, _ := os.ReadDir(path)
	for _, entry := range entries {
		if kind == "wifi" {
			if _, err := os.Stat(filepath.Join(path, entry.Name(), "wireless")); err != nil {
				continue
			}
		} else if !hciName.MatchString(entry.Name()) {
			continue
		}
		out = append(out, RadioDevice{ID: entry.Name(), Name: entry.Name()})
	}
	return out
}

func (d *linuxConnectivity) Get(ctx context.Context) Connectivity {
	status := Connectivity{Source: "host", Platform: "linux", WiFi: d.wifi(ctx), Bluetooth: d.bluetooth(ctx), Interfaces: readHostInterfaces(d.root)}
	// Read active profile names, without scanning for access points or changing
	// connections. Profiles may have custom names; never label these as SSIDs.
	if _, err := d.lookPath("nmcli"); err == nil {
		for n := range status.Interfaces {
			item := &status.Interfaces[n]
			if item.State != "up" || item.Type == "other" {
				continue
			}
			if out, err := d.run(ctx, "nmcli", []string{"--get-values", "GENERAL.CONNECTION", "device", "show", item.Name}, ""); err == nil {
				name := strings.TrimSpace(string(out))
				if name != "--" {
					item.Connection = name
				}
				if item.Type == "wifi" && status.WiFi.Connection == "" {
					status.WiFi.Connection = item.Connection
				}
			}
		}
	}
	return status
}

func (d *linuxConnectivity) wifi(ctx context.Context) RadioStatus {
	s := noRadio()
	s.Devices = d.devices("wifi")
	s.Present = len(s.Devices) > 0
	if !s.Present {
		return s
	}
	if _, err := d.lookPath("nmcli"); err != nil {
		return radioFailure(s, "missing_tool", "NetworkManager nmcli is not installed")
	}
	out, err := d.run(ctx, "nmcli", []string{"--terse", "--fields", "WIFI-HW,WIFI", "general"}, "")
	if err != nil {
		return radioFailure(s, radioErrorReason(err), err.Error())
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(parts) != 2 || (parts[0] != "enabled" && parts[0] != "disabled") || (parts[1] != "enabled" && parts[1] != "disabled") {
		return radioFailure(s, "query_failed", "NetworkManager returned an unknown Wi-Fi radio state")
	}
	if parts[0] == "disabled" {
		s = radioValue(s, false)
		s.Supported = false
		s.Reason = "hardware_blocked"
		s.Detail = "Wi-Fi is disabled by a hardware switch or host policy"
		return s
	}
	return radioValue(s, parts[1] == "enabled")
}

func (d *linuxConnectivity) controllers(ctx context.Context) ([]RadioDevice, error) {
	out, err := d.run(ctx, "bluetoothctl", []string{"--timeout", "5", "list"}, "")
	if err != nil {
		return nil, err
	}
	devices := []RadioDevice{}
	for _, match := range controllerLine.FindAllStringSubmatch(string(out), -1) {
		devices = append(devices, RadioDevice{ID: match[1], Name: strings.TrimSpace(match[2])})
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("BlueZ did not report a host Bluetooth controller")
	}
	return devices, nil
}

func (d *linuxConnectivity) bluetooth(ctx context.Context) RadioStatus {
	s := noRadio()
	s.Devices = d.devices("bluetooth")
	s.Present = len(s.Devices) > 0
	if !s.Present {
		return s
	}
	if _, err := d.lookPath("bluetoothctl"); err != nil {
		return radioFailure(s, "missing_tool", "BlueZ bluetoothctl is not installed")
	}
	devices, err := d.controllers(ctx)
	if err != nil {
		return radioFailure(s, radioErrorReason(err), err.Error())
	}
	s.Devices = devices
	enabled := false
	for _, device := range devices {
		out, err := d.run(ctx, "bluetoothctl", []string{"--timeout", "5", "show", device.ID}, "")
		if err != nil {
			return radioFailure(s, radioErrorReason(err), err.Error())
		}
		found := false
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "Powered: yes" {
				enabled = true
				found = true
			}
			if line == "Powered: no" {
				found = true
			}
		}
		if !found {
			return radioFailure(s, "query_failed", "BlueZ returned an unknown controller power state")
		}
	}
	return radioValue(s, enabled)
}

func (d *linuxConnectivity) Set(ctx context.Context, kind string, enabled bool) error {
	state := "off"
	if enabled {
		state = "on"
	}
	if kind == "wifi" {
		_, err := d.run(ctx, "nmcli", []string{"radio", "wifi", state}, "")
		return err
	}
	devices, err := d.controllers(ctx)
	if err != nil {
		return err
	}
	for _, device := range devices {
		// Controller addresses come only from validated BlueZ output. This is
		// bluetoothctl's own input protocol, with no shell interpretation.
		_, err = d.run(ctx, "bluetoothctl", []string{"--timeout", "5"}, "select "+device.ID+"\npower "+state+"\nquit\n")
		if err != nil {
			return rpc.Errorf(rpc.Unavailable, "Bluetooth power request failed: %v", err)
		}
	}
	return nil
}
