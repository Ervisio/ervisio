package system

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

type windowsConnectivity struct {
	lookPath func(string) (string, error)
	run      radioRunner
}

// WinRT enumerates RadioKind.WiFi/Bluetooth hardware radios, not PnP Bluetooth
// peripherals. Get-NetAdapter metadata labels physical Ethernet interfaces.
// https://learn.microsoft.com/uwp/api/windows.devices.radios.radio.getradiosasync
const windowsRadioPrelude = `
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
function Await-Radio($Operation, [Type]$ResultType) {
  $method = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.IsGenericMethod -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq ('IAsyncOperation' + [char]96 + '1') } | Select-Object -First 1
  $task = $method.MakeGenericMethod($ResultType).Invoke($null, @($Operation))
  if (-not $task.Wait(5000)) { throw 'The Windows radio query timed out.' }
  return $task.Result
}
function Load-Radios {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Devices.Radios.Radio, Windows.System.Devices, ContentType=WindowsRuntime]
  return @(Await-Radio ([Windows.Devices.Radios.Radio]::GetRadiosAsync()) ([System.Collections.Generic.IReadOnlyList[Windows.Devices.Radios.Radio]]))
}
`

const windowsRadioQuery = windowsRadioPrelude + `
$result = @{ radios = @(); interfaces = @() }
try {
  $radios = @(Load-Radios)
  $result.radios = @($radios | ForEach-Object { @{ kind=$_.Kind.ToString(); name=$_.Name; state=$_.State.ToString() } })
} catch { $result.error = $_.Exception.Message }
try {
  $profiles = @(Get-NetConnectionProfile -ErrorAction SilentlyContinue)
  $result.interfaces = @(Get-NetAdapter -IncludeHidden | ForEach-Object {
    $kind = 'other'
    if ($_.HardwareInterface -and ([int]$_.InterfaceType -eq 71 -or [int]$_.NdisPhysicalMedium -in @(1,9))) { $kind = 'wifi' }
    elseif ($_.HardwareInterface -and [int]$_.InterfaceType -eq 6) { $kind = 'ethernet' }
    $index = $_.ifIndex
    $profile = $profiles | Where-Object InterfaceIndex -eq $index | Select-Object -First 1
    @{ index=[int]$index; name=$_.Name; type=$kind; connection=if($profile){$profile.Name}else{''} }
  })
} catch { }
$result | ConvertTo-Json -Compress -Depth 5
`

type windowsRadioProbe struct {
	Radios []struct {
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		State string `json:"state"`
	} `json:"radios"`
	Interfaces []struct {
		Index      int    `json:"index"`
		Name       string `json:"name"`
		Type       string `json:"type"`
		Connection string `json:"connection"`
	} `json:"interfaces"`
	Error string `json:"error"`
}

func (d *windowsConnectivity) Get(ctx context.Context) Connectivity {
	status := Connectivity{Source: "host", Platform: "windows", WiFi: noRadio(), Bluetooth: noRadio(), Interfaces: readHostInterfaces("")}
	if _, err := d.lookPath("powershell.exe"); err != nil {
		status.WiFi = radioFailure(status.WiFi, "missing_tool", "Windows PowerShell is unavailable")
		status.Bluetooth = radioFailure(status.Bluetooth, "missing_tool", "Windows PowerShell is unavailable")
		return status
	}
	out, err := d.run(ctx, "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Mta", "-Command", windowsRadioQuery}, "")
	if err != nil {
		status.WiFi = radioFailure(status.WiFi, radioErrorReason(err), err.Error())
		status.Bluetooth = radioFailure(status.Bluetooth, radioErrorReason(err), err.Error())
		return status
	}
	var probe windowsRadioProbe
	if err := json.Unmarshal(out, &probe); err != nil {
		status.WiFi = radioFailure(status.WiFi, "query_failed", "Windows returned invalid radio information")
		status.Bluetooth = radioFailure(status.Bluetooth, "query_failed", "Windows returned invalid radio information")
		return status
	}
	for _, meta := range probe.Interfaces {
		for n := range status.Interfaces {
			item := &status.Interfaces[n]
			if item.ID != fmt.Sprint(meta.Index) {
				continue
			}
			if meta.Type == "wifi" || meta.Type == "ethernet" {
				item.Type = meta.Type
			}
			item.Connection = meta.Connection
		}
	}
	sortHostInterfaces(status.Interfaces)
	if probe.Error != "" {
		err := fmt.Errorf("%s", probe.Error)
		status.WiFi = radioFailure(status.WiFi, radioErrorReason(err), probe.Error)
		status.Bluetooth = radioFailure(status.Bluetooth, radioErrorReason(err), probe.Error)
		return status
	}
	status.WiFi = probe.radio("WiFi")
	status.Bluetooth = probe.radio("Bluetooth")
	for _, item := range status.Interfaces {
		if item.Type == "wifi" && item.State == "up" && status.WiFi.Connection == "" {
			status.WiFi.Connection = item.Connection
		}
	}
	return status
}

func (p windowsRadioProbe) radio(kind string) RadioStatus {
	s := noRadio()
	enabled := false
	blocked := false
	for n, item := range p.Radios {
		if item.Kind != kind {
			continue
		}
		s.Present = true
		s.Devices = append(s.Devices, RadioDevice{ID: fmt.Sprintf("%s-%d", strings.ToLower(kind), n), Name: item.Name})
		switch item.State {
		case "On":
			enabled = true
		case "Off":
		case "Disabled":
			blocked = true
		default:
			return radioFailure(s, "query_failed", "Windows reported an unknown radio state")
		}
	}
	if !s.Present {
		return s
	}
	if blocked {
		s = radioFailure(s, "hardware_blocked", "Windows disabled this radio by hardware switch or system policy")
		return s
	}
	return radioValue(s, enabled)
}

func (d *windowsConnectivity) Set(ctx context.Context, kind string, enabled bool) error {
	winKind := "WiFi"
	if kind == "bluetooth" {
		winKind = "Bluetooth"
	}
	state := "Off"
	if enabled {
		state = "On"
	}
	// Only fixed enum values enter this script, never user text.
	script := windowsRadioPrelude + `
try {
  $radios = @(Load-Radios | Where-Object { $_.Kind.ToString() -eq '` + winKind + `' })
  if (-not $radios.Count) { throw 'No matching host radio is available.' }
  $access = Await-Radio ([Windows.Devices.Radios.Radio]::RequestAccessAsync()) ([Windows.Devices.Radios.RadioAccessStatus])
  if ($access.ToString() -ne 'Allowed') { throw ('Radio access denied: ' + $access) }
  foreach ($radio in $radios) {
    $result = Await-Radio ($radio.SetStateAsync([Windows.Devices.Radios.RadioState]::` + state + `)) ([Windows.Devices.Radios.RadioAccessStatus])
    if ($result.ToString() -ne 'Allowed') { throw ('Radio change denied: ' + $result) }
  }
  for($attempt=0;$attempt -lt 15;$attempt++) {
    $pending = @($radios | Where-Object { $_.State.ToString() -ne '` + state + `' })
    if (-not $pending.Count) { break }
    Start-Sleep -Milliseconds 100
  }
  if ($pending.Count) { throw 'The radio state did not change; a hardware switch or host policy may block it.' }
  @{ok=$true} | ConvertTo-Json -Compress
} catch { Write-Error $_.Exception.Message; exit 1 }
`
	_, err := d.run(ctx, "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Mta", "-Command", script}, "")
	if err != nil && radioErrorReason(err) == "permission_denied" {
		return rpc.Errorf(rpc.Forbidden, "Windows refused radio access: %v", err)
	}
	return err
}
