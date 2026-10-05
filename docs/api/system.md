# system.* (Overview)

Package `server/internal/modules/system`. All sizes are bytes, rates bytes/second,
percentages 0–100 with one decimal.

## `system.host` (user) → Host

```json
{"hostname":"arch",
 "distro":{"id":"arch","name":"Arch Linux","prettyName":"Arch Linux","version":"","color":"#1793D1","logo":"archlinux-logo"},
 "kernel":"7.2.6-arch2-1","arch":"x86_64",
 "cpu":{"model":"12th Gen Intel(R) Core(TM) i7-12650H","cores":10,"threads":16},
 "memoryTotal":16448176128,
 "uptime":7195, "bootTime":1790871681,
 "ip":"192.168.1.12",
 "machine":{"vendor":"Micro-Star International Co., Ltd.","product":"Katana 15 B12UDXK"}}
```
`uptime` is seconds, `bootTime` unix seconds. `ip` (primary outbound address), `version`,
`logo`, `machine.*` may be empty/omitted. `distro.color` comes from the distro colour table
(default `#3AB4F2`).

## `system.metrics` (user) → Metrics

```json
{"time":1790878876469, "interval":0.251,
 "cpu":{"percent":10.2,"cores":[4.2,0,12,…]},
 "load":[1.32,1.94,4.03],
 "memory":{"total":…,"used":…,"available":…,"free":…,"buffers":…,"cached":…,"percent":52},
 "swap":{"total":…,"used":…,"free":…,"percent":49.2},
 "disks":[{"mount":"/","device":"/dev/mapper/root","fstype":"btrfs","total":…,"used":…,"free":…,"percent":19.8}],
 "net":[{"iface":"wlo1","rxBytes":676485514,"txBytes":600435325,"rxRate":7536.4,"txRate":3089.6,"virtual":false,"up":true}]}
```
- `time` unix ms; `interval` = seconds between the two samples used for CPU/net rates. The first
  call (or after 30 s idle) takes two samples 250 ms apart.
- `memory.used = total − available`; `cached` includes `SReclaimable`.
- `disks`: real filesystems only (ext4, btrfs, xfs, vfat, nfs…), one entry per device (btrfs
  subvolumes collapse to the shortest mount point). `free` = available to unprivileged users,
  `percent = used / (used + free)`.
- `net`: every interface except `lo`; physical first. `virtual` = no `/sys/class/net/<if>/device`
  (bridges, docker, veth, tun). `up` = operstate is `up`. `cores`, `disks`, `net` are always arrays.

## `system.metricsStream` (user, stream)

Params `{"interval": 2000}` (ms, default 2000, clamped 250…600000). Emits one Metrics object per
interval as `data` (first one immediately). Never ends by itself; close the channel to stop.

```
→ {"ch":1,"op":"open","method":"system.metricsStream","params":{"interval":1000}}
← {"ch":1,"op":"data","data":{…Metrics…}}
```

## `system.power` (admin)

Params `{"action":"reboot"|"poweroff"}` → `{"ok":true}` (runs `systemctl reboot|poweroff`).

## Windows notes

`system.host` and `system.metrics` keep the same shapes on Windows, filled from Win32 instead of `/proc`:

- CPU: `GetSystemTimes` deltas (aggregate) and `NtQuerySystemInformation` (per core).
- Memory: `GlobalMemoryStatusEx`; `buffers`/`cached` are 0. Swap is the commit limit minus physical memory (page files).
- `load` is always `[0,0,0]` (no load average on Windows).
- Disks: fixed drives only (mount `C:\`), file system from `GetVolumeInformation`.
- Network: `GetIfTable2` octet counters, loopback skipped; `virtual` means not a hardware interface.
- Host: distro id `windows` (name from the registry, Windows 11 detected by build >= 22000), `kernel` is `10.0.<build>.<UBR>`, machine from the BIOS registry key, uptime from `GetTickCount64`. Not tested on a real Windows host.
