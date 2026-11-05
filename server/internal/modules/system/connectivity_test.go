package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func radioTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{"sys/class/net/wlp1/wireless", "sys/class/net/eth0/device", "sys/class/net/veth0", "sys/class/bluetooth/hci0", "sys/class/bluetooth/hci0-peripheral"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "sys/class/net/eth0/type"), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func foundTool(name string) (string, error) { return "/usr/bin/" + name, nil }

func TestLinuxRadioDetection(t *testing.T) {
	d := &linuxConnectivity{root: radioTree(t), lookPath: foundTool, run: func(_ context.Context, name string, args []string, input string) ([]byte, error) {
		if input != "" {
			t.Fatal("read attempted to change a radio")
		}
		switch name + " " + strings.Join(args, " ") {
		case "nmcli --terse --fields WIFI-HW,WIFI general":
			return []byte("enabled:disabled\n"), nil
		case "bluetoothctl --timeout 5 list":
			return []byte("Controller 00:11:22:33:44:55 Host radio [default]\n"), nil
		case "bluetoothctl --timeout 5 show 00:11:22:33:44:55":
			return []byte("Controller 00:11:22:33:44:55\n\tPowered: yes\n"), nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
			return nil, nil
		}
	}}
	wifi, bt := d.wifi(context.Background()), d.bluetooth(context.Background())
	if !wifi.Present || !wifi.Supported || wifi.Enabled == nil || *wifi.Enabled || wifi.State != "off" {
		t.Fatalf("wifi: %+v", wifi)
	}
	if !bt.Present || !bt.Supported || bt.Enabled == nil || !*bt.Enabled || len(bt.Devices) != 1 || bt.Devices[0].Name != "Host radio" {
		t.Fatalf("bluetooth: %+v", bt)
	}
	for name, want := range map[string]string{"wlp1": "wifi", "eth0": "ethernet", "veth0": "other"} {
		if got := linuxInterfaceType(d.root, name); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
	if got := len(d.devices("bluetooth")); got != 1 {
		t.Fatalf("Bluetooth peripherals mistaken for radios: %d", got)
	}
}

func TestMissingHardwareAndTools(t *testing.T) {
	d := &linuxConnectivity{root: t.TempDir(), lookPath: foundTool, run: func(context.Context, string, []string, string) ([]byte, error) {
		t.Fatal("queried missing hardware")
		return nil, nil
	}}
	if s := d.wifi(context.Background()); s.Present || s.Supported || s.Enabled != nil || s.Reason != "no_hardware" {
		t.Fatalf("%+v", s)
	}
	d.root = radioTree(t)
	d.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	for _, s := range []RadioStatus{d.wifi(context.Background()), d.bluetooth(context.Background())} {
		if !s.Present || s.Supported || s.Enabled != nil || s.Reason != "missing_tool" {
			t.Fatalf("%+v", s)
		}
	}
}

func TestRadioHardwareBlockAndQueryFailure(t *testing.T) {
	d := &linuxConnectivity{root: radioTree(t), lookPath: foundTool}
	d.run = func(context.Context, string, []string, string) ([]byte, error) {
		return []byte("disabled:disabled"), nil
	}
	if s := d.wifi(context.Background()); s.Supported || s.Enabled == nil || *s.Enabled || s.Reason != "hardware_blocked" {
		t.Fatalf("%+v", s)
	}
	d.run = func(context.Context, string, []string, string) ([]byte, error) {
		return nil, errors.New("Permission denied")
	}
	if s := d.wifi(context.Background()); s.Supported || s.Enabled != nil || s.Reason != "permission_denied" {
		t.Fatalf("%+v", s)
	}
	d.run = func(context.Context, string, []string, string) ([]byte, error) { return []byte("unrecognized"), nil }
	if s := d.wifi(context.Background()); s.Supported || s.Enabled != nil || s.Reason != "query_failed" {
		t.Fatalf("%+v", s)
	}
}

func TestLinuxRadioSetUsesFixedCommands(t *testing.T) {
	calls := []string{}
	d := &linuxConnectivity{root: radioTree(t), lookPath: foundTool, run: func(_ context.Context, name string, args []string, input string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " ")+"|"+input)
		if len(args) > 2 && args[2] == "list" {
			return []byte("Controller 00:11:22:33:44:55 Host [default]\n"), nil
		}
		return nil, nil
	}}
	if err := d.Set(context.Background(), "wifi", false); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(context.Background(), "bluetooth", true); err != nil {
		t.Fatal(err)
	}
	want := []string{"nmcli radio wifi off|", "bluetoothctl --timeout 5 list|", "bluetoothctl --timeout 5|select 00:11:22:33:44:55\npower on\nquit\n"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("%q", calls)
	}
}

func TestWindowsRadiosAreRealRadioKinds(t *testing.T) {
	d := &windowsConnectivity{lookPath: foundTool, run: func(_ context.Context, name string, args []string, input string) ([]byte, error) {
		if name != "powershell.exe" || input != "" || strings.Contains(args[len(args)-1], "SetStateAsync") || strings.Contains(args[len(args)-1], "RequestAccessAsync") {
			t.Fatal("read performed a mutation")
		}
		return []byte(`{"radios":[{"kind":"WiFi","name":"Wireless adapter","state":"Off"},{"kind":"Bluetooth","name":"Host Bluetooth","state":"On"},{"kind":"Headphones","name":"Peripheral","state":"On"}],"interfaces":[]}`), nil
	}}
	s := d.Get(context.Background())
	if !s.WiFi.Present || !s.WiFi.Supported || s.WiFi.Enabled == nil || *s.WiFi.Enabled {
		t.Fatalf("%+v", s)
	}
	if len(s.Bluetooth.Devices) != 1 || s.Bluetooth.Enabled == nil || !*s.Bluetooth.Enabled {
		t.Fatalf("%+v", s)
	}
	var probe windowsRadioProbe
	_ = json.Unmarshal([]byte(`{"radios":[{"kind":"WiFi","name":"Adapter","state":"Disabled"}]}`), &probe)
	if r := probe.radio("WiFi"); r.Supported || r.Enabled != nil || r.Reason != "hardware_blocked" {
		t.Fatalf("%+v", r)
	}
	if r := probe.radio("Bluetooth"); r.Reason != "no_hardware" {
		t.Fatalf("%+v", r)
	}
}

type fakeConnectivity struct {
	state  Connectivity
	sets   int
	ignore bool
}

func (f *fakeConnectivity) Get(context.Context) Connectivity { return f.state }
func (f *fakeConnectivity) Set(_ context.Context, kind string, value bool) error {
	f.sets++
	if !f.ignore {
		if kind == "wifi" {
			f.state.WiFi = radioValue(f.state.WiFi, value)
		} else {
			f.state.Bluetooth = radioValue(f.state.Bluetooth, value)
		}
	}
	return nil
}

func TestConnectivityPermissionValidationAndReadback(t *testing.T) {
	f := &fakeConnectivity{state: Connectivity{WiFi: radioValue(RadioStatus{Present: true}, true), Bluetooth: noRadio()}}
	r := rpc.NewRegistry()
	registerConnectivity(r, f)
	if r.Methods()["system.connectivity"] != rpc.User || r.Methods()["system.connectivity.set"] != rpc.Admin {
		t.Fatal("radio permissions lost")
	}
	h := connectivitySetter(f)
	for _, params := range []string{`{}`, `{"radio":"wifi"}`, `{"radio":"wifi;command","enabled":true}`, `{"radio":"wifi","enabled":"on"}`} {
		if _, err := h(context.Background(), &rpc.Call{Params: json.RawMessage(params)}); !rpc.IsCode(err, rpc.Invalid) {
			t.Fatalf("invalid request accepted: %s %v", params, err)
		}
	}
	if f.sets != 0 {
		t.Fatal("invalid parameters reached hardware")
	}
	if _, err := h(context.Background(), &rpc.Call{Params: json.RawMessage(`{"radio":"bluetooth","enabled":true}`)}); !rpc.IsCode(err, rpc.Unavailable) {
		t.Fatal("missing hardware toggle accepted", err)
	}
	f.ignore = true
	if _, err := h(context.Background(), &rpc.Call{Params: json.RawMessage(`{"radio":"wifi","enabled":false}`)}); !rpc.IsCode(err, rpc.Conflict) {
		t.Fatal("unconfirmed toggle reported success", err)
	}
	f.ignore = false
	if _, err := h(context.Background(), &rpc.Call{Params: json.RawMessage(`{"radio":"wifi","enabled":false}`)}); err != nil {
		t.Fatal(fmt.Sprint(err))
	}
}
