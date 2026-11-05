//go:build windows

package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
)

// All executable PowerShell is static. Device/label input is JSON in a separate environment value.
const windowsGuard = `
function Assert-DataVolume($v) {
 $parts=@(Get-Partition -Volume $v -ErrorAction Stop)
 if($parts.Count -ne 1){throw 'Only a simple local data partition can be formatted.'}
 $part=$parts[0]; $disk=Get-Disk -Number $part.DiskNumber -ErrorAction Stop
 if($part.IsBoot -or $part.IsSystem -or $disk.IsOffline -or $disk.IsReadOnly){throw 'System, offline or read-only volume.'}
 if($part.GptType -in @('{c12a7328-f81f-11d2-ba4b-00a0c93ec93b}','{de94bba4-06d1-4d40-a16a-bfd50179d6ac}','{e3c9e316-0b5c-4db8-817d-f92df00215ae}') -or $part.MbrType -eq 39){throw 'Reserved or recovery partition.'}
 if($v.DriveLetter -and (($v.DriveLetter+':') -eq $env:SystemDrive)){throw 'The operating system volume cannot be formatted.'}
 foreach($page in @(Get-CimInstance Win32_PageFileUsage -ErrorAction Stop)){if($v.DriveLetter -and $page.Name.StartsWith($v.DriveLetter+':',[StringComparison]::OrdinalIgnoreCase)){throw 'The volume contains an active page file.'}}
 if([string]$v.DriveType -notin @('Fixed','Removable')){throw 'Only a local data volume can be formatted.'}
 return [string]$disk.UniqueId+'|'+[string]$part.Guid+'|'+[string]$part.Offset
}
`

func inventory(ctx context.Context) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	script := `$ErrorActionPreference='Stop';` + windowsGuard + `
 $out=@();foreach($v in @(Get-Volume)){
  if(-not $v.UniqueId){continue};$blocked='';$native=''
  try{$native=Assert-DataVolume $v}catch{$blocked='system_or_busy_volume'}
  $device=if($v.DriveLetter){[string]$v.DriveLetter+':'}else{[string]$v.UniqueId}
  $mounts=@();if($v.DriveLetter){$mounts=@([string]$v.DriveLetter+':\')}
  $out+=@{device=$device;name=$device;size=[uint64]$v.Size;fstype=[string]$v.FileSystemType;label=[string]$v.FileSystemLabel;mounts=$mounts;blocked=$blocked;volume=[string]$v.UniqueId;native=$native}
 };ConvertTo-Json -InputObject @($out) -Depth 4 -Compress`
	output, err := (sys.Cmd{Name: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-Command", script}}).Output(ctx)
	if err != nil {
		return nil, rpc.Errorf(rpc.Unavailable, "Local Windows volumes cannot be inspected: %v", err)
	}
	var rows []struct {
		Device  string
		Name    string
		Size    uint64
		FSType  string
		Label   string
		Mounts  []string
		Blocked string
		Volume  string
		Native  string
	}
	if err = json.Unmarshal(output, &rows); err != nil {
		return nil, err
	}
	out := []Device{}
	for _, r := range rows {
		out = append(out, finishDevice(Device{Device: r.Device, Name: r.Name, Type: "volume", Size: r.Size, FSType: r.FSType, Label: r.Label, Mounts: r.Mounts, Blocked: r.Blocked, NativeID: r.Volume, Serial: r.Native, Filesystems: []string{"ntfs", "exfat", "fat32"}, Platform: "windows"}))
	}
	return out, nil
}
func sameDevice(d Device, name string) bool {
	return strings.EqualFold(strings.TrimRight(d.Device, `\/`), strings.TrimRight(name, `\/`))
}
func formatVolume(ctx context.Context, d Device, p formatRequest, send func(any) error) error {
	fresh, err := inspect(ctx, d.Device)
	if err != nil {
		return err
	}
	if fresh.Identity != d.Identity {
		return rpc.Errorf(rpc.Conflict, "The volume changed. Inspect it again.")
	}
	payload, _ := json.Marshal(map[string]any{"volume": d.NativeID, "native": d.Serial, "size": d.Size, "filesystem": strings.ToUpper(p.Filesystem), "label": p.Label, "quick": p.Quick, "force": p.Force})
	script := `$ErrorActionPreference='Stop';` + windowsGuard + `
 $p=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:ERVISIO_FORMAT_REQUEST))|ConvertFrom-Json
 $found=@(Get-Volume|Where-Object{$_.UniqueId -eq $p.volume});if($found.Count -ne 1){throw 'The selected volume changed.'}
 $v=$found[0];$native=Assert-DataVolume $v
 if($native -ne $p.native -or [uint64]$v.Size -ne [uint64]$p.size){throw 'The selected disk identity changed.'}
 if($p.filesystem -notin @('NTFS','EXFAT','FAT32')){throw 'Unsupported filesystem.'}
 $args=@{InputObject=$v;FileSystem=$p.filesystem;NewFileSystemLabel=$p.label;Confirm=$false;ErrorAction='Stop'}
 if(-not $p.quick){$args.Full=$true};if($p.force){$args.Force=$true}
 Format-Volume @args|Out-Null`
	if err = send(Event{Phase: "format", Device: d.Device}); err != nil {
		return err
	}
	err = (sys.Cmd{Name: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-Command", script}, Env: []string{"ERVISIO_FORMAT_REQUEST=" + base64.StdEncoding.EncodeToString(payload)}, Timeout: -1}).Run(ctx)
	if err != nil {
		return errorText(err)
	}
	return nil
}
