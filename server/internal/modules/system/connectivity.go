package system

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// RadioDevice is a host radio adapter, never a paired Bluetooth peripheral.
type RadioDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RadioStatus distinguishes unavailable hardware from a radio which is off.
// Enabled is nil when the host cannot establish its actual state.
type RadioStatus struct {
	Present    bool          `json:"present"`
	Supported  bool          `json:"supported"`
	Enabled    *bool         `json:"enabled"`
	State      string        `json:"state"`
	Reason     string        `json:"reason,omitempty"`
	Detail     string        `json:"detail,omitempty"`
	Devices    []RadioDevice `json:"devices"`
	Connection string        `json:"connection,omitempty"`
}

// HostInterface reports link state and addresses of the managed host.
type HostInterface struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	State      string   `json:"state"`
	Addresses  []string `json:"addresses"`
	Connection string   `json:"connection,omitempty"`
}

// Connectivity is always about the managed host, not the browser's machine.
type Connectivity struct {
	Source     string          `json:"source"`
	Platform   string          `json:"platform"`
	WiFi       RadioStatus     `json:"wifi"`
	Bluetooth  RadioStatus     `json:"bluetooth"`
	Interfaces []HostInterface `json:"interfaces"`
}

func readHostInterfaces(root string) []HostInterface {
	result := []HostInterface{}
	interfaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		item := HostInterface{ID: strconv.Itoa(i.Index), Name: i.Name, Type: "other", State: "down", Addresses: []string{}}
		if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagRunning != 0 {
			item.State = "up"
		}
		if addrs, err := i.Addrs(); err == nil {
			for _, addr := range addrs {
				item.Addresses = append(item.Addresses, addr.String())
			}
		}
		if root != "" {
			item.Type = linuxInterfaceType(root, i.Name)
		}
		result = append(result, item)
	}
	sortHostInterfaces(result)
	return result
}

func sortHostInterfaces(items []HostInterface) {
	weight := func(item HostInterface) int {
		n := 0
		if item.State != "up" {
			n += 4
		}
		switch item.Type {
		case "ethernet":
		case "wifi":
			n++
		default:
			n += 2
		}
		return n
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := weight(items[i]), weight(items[j])
		if a != b {
			return a < b
		}
		return items[i].Name < items[j].Name
	})
}

func linuxInterfaceType(root, name string) string {
	base := filepath.Join(root, "sys/class/net", name)
	if _, err := os.Stat(filepath.Join(base, "wireless")); err == nil {
		return "wifi"
	}
	if _, err := os.Stat(filepath.Join(base, "device")); err == nil {
		if data, err := os.ReadFile(filepath.Join(base, "type")); err == nil && strings.TrimSpace(string(data)) == "1" {
			return "ethernet"
		}
	}
	return "other"
}

type connectivityDriver interface {
	Get(context.Context) Connectivity
	Set(context.Context, string, bool) error
}

// radioRunner takes argv and optional protocol input, never a shell command.
type radioRunner func(context.Context, string, []string, string) ([]byte, error)

func runRadio(ctx context.Context, name string, args []string, input string) ([]byte, error) {
	return (sys.Cmd{Name: name, Args: args, Stdin: strings.NewReader(input), Timeout: 8 * time.Second, MaxOutput: 128 << 10}).Output(ctx)
}

func registerConnectivity(r *rpc.Registry, driver connectivityDriver) {
	r.Handle("system.connectivity", rpc.User, func(ctx context.Context, c *rpc.Call) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return driver.Get(ctx), nil
	})
	r.Handle("system.connectivity.set", rpc.Admin, connectivitySetter(driver))
}

func connectivitySetter(driver connectivityDriver) rpc.Handler {
	return func(ctx context.Context, c *rpc.Call) (any, error) {
		var p struct {
			Radio   string `json:"radio"`
			Enabled *bool  `json:"enabled"`
		}
		if err := c.Bind(&p); err != nil {
			return nil, err
		}
		if (p.Radio != "wifi" && p.Radio != "bluetooth") || p.Enabled == nil {
			return nil, rpc.Errorf(rpc.Invalid, "radio must be wifi or bluetooth and enabled must be a boolean")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		before := driver.Get(ctx)
		radio := before.WiFi
		if p.Radio == "bluetooth" {
			radio = before.Bluetooth
		}
		if !radio.Supported {
			return nil, rpc.Errorf(rpc.Unavailable, "%s: %s (%s)", p.Radio, radio.Reason, radio.Detail)
		}
		if err := driver.Set(ctx, p.Radio, *p.Enabled); err != nil {
			return nil, err
		}
		after := driver.Get(ctx)
		radio = after.WiFi
		if p.Radio == "bluetooth" {
			radio = after.Bluetooth
		}
		if radio.Enabled == nil || *radio.Enabled != *p.Enabled {
			return nil, rpc.Errorf(rpc.Conflict, "the host did not reach the requested %s state; refresh to inspect its actual state", p.Radio)
		}
		return after, nil
	}
}

func noRadio() RadioStatus {
	return RadioStatus{State: "unavailable", Reason: "no_hardware", Devices: []RadioDevice{}}
}

func radioFailure(s RadioStatus, reason, detail string) RadioStatus {
	s.Supported = false
	s.Enabled = nil
	s.State = "unknown"
	s.Reason = reason
	s.Detail = detail
	return s
}

func radioErrorReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "query_failed"
	}
	t := strings.ToLower(err.Error())
	if strings.Contains(t, "access") || strings.Contains(t, "permission") || strings.Contains(t, "not authorized") || strings.Contains(t, "denied") {
		return "permission_denied"
	}
	return "query_failed"
}

func radioValue(s RadioStatus, enabled bool) RadioStatus {
	s.Supported = true
	s.Enabled = &enabled
	s.State = "off"
	if enabled {
		s.State = "on"
	}
	s.Reason, s.Detail = "", ""
	return s
}
