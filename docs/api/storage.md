# Local volume formatting

Storage formatting operates on local non-system volumes only. It does not format
SFTP/SMB shares and does not install a filesystem formatter on the user's behalf.
The UI exposes it through a local drive's context menu in This PC, in both shells.

## Inspection

`storage.list` (User) returns `{devices:[Device]}`; `storage.inspect {device}`
(User) returns one current `Device`:

```json
{"device":"/dev/sdb1","name":"sdb1","type":"part","size":1073741824,
 "fstype":"ext4","label":"Data","mounts":["/mnt/data"],"readOnly":false,
 "identity":"opaque-sha256-token","confirmation":"FORMAT /dev/sdb1",
 "filesystems":["ext4","exfat","fat32","ntfs"],"platform":"linux"}
```

`blocked` is set when a system, reserved/recovery, read-only, swap, busy/managed,
partitioned parent disk or otherwise unsupported device cannot be formatted.
Linux reports only filesystem formatters actually installed on the host.
Device metadata comes from the operating system; the frontend cannot invent an
eligible device by supplying a different path.

## Formatting stream

`storage.format` is Admin and requires the current inspection token plus the exact
confirmation string returned by the server:

```json
{"device":"/dev/sdb1","identity":"opaque-sha256-token","filesystem":"ext4",
 "label":"Data","quick":true,"force":false,"confirm":"FORMAT /dev/sdb1"}
```

The server repeats inspection and validation before writing. Only one format can
run at a time. Replaced, resized, remounted or otherwise changed devices are
refused. Labels are bounded, validated strings passed through argument arrays
on Linux and a JSON environment value to static PowerShell on Windows.

Linux uses block-device identity checks, an advisory lock and exclusive open
checks. System mounts cannot be unmounted. The explicit unmount option uses
ordinary `umount`, so busy mounts fail instead of being lazily or forcibly
detached. Full format first overwrites the selected volume with bounded buffers;
quick format initializes filesystem metadata without that overwrite pass.
Windows uses the native `Format-Volume` command after checking the volume,
partition and disk identities, system/reserved status and page-file use; the full
and force options are passed only when requested. No shell expression is built
from a device name or label.

Events are `{phase,percent?,message?,device?}`. Phases are `check`, optional
`unmount`, optional `erase`, `format`, and `done`. Errors stop the stream. Closing
a stream cancels work; an interrupted format may leave the selected volume
unusable. The UI therefore prevents accidental window close while work runs.

## Guarded integration test

Ordinary tests cover option validation, protected devices, changing identities
and bounded erasure using temporary files. Linux additionally has an explicit
`ERVISIO_STORAGE_LOOP_TEST=1` / root-only integration test. It creates a new
private 64 MiB image and its own loop device, checks the exact sysfs backing file
before each format/detach, verifies quick and full ext4 formatting with `blkid`,
and detaches only that owned loop. It never chooses an existing physical device,
mounts test data or loads a kernel module. Environments without loop support skip
this test. Windows formatting is not invoked by the test suite.
