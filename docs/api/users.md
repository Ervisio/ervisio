# users.* and groups.* (Users section)

Package `server/internal/modules/users`. Accounts are read from `/etc/passwd`, `/etc/group`, `/etc/login.defs`
(UID_MIN, GID_MIN) and `/etc/shells` directly; `/etc/shadow` only when the bridge runs as root. Changes run the
shadow-utils tools as argv (`useradd`, `usermod`, `userdel`, `gpasswd`, `groupadd`, `groupdel`, `chage`) and
`chpasswd` with `name:password` on **stdin** (a password is never in argv).

Validation: user and group names match `^[a-z_][a-z0-9_-]{0,31}$`; shells must be listed in `/etc/shells`;
full names (GECOS) cannot contain `: , = \` or control characters (max 128); passwords are 1-256 characters without
line breaks. Failed tools return their message, e.g. `invalid`/`conflict`/`internal` with a human sentence.

"People" = UID >= UID_MIN (and < 65534) plus root. Everything else is a "system account". Only people (and root for
password/lock/shell/group changes) can be modified; system accounts answer `forbidden`.

## Read methods (user level)

| Method | Params | Result |
|---|---|---|
| `users.list` | `{}` | `{people: User[], system: User[], uidMin, shadowReadable, lastLoginKnown, adminGroup, shells[], self}` |
| `users.groups` | `{}` | `Group[]` sorted by gid |
| `users.sessions` | `{name?}` | `Session[]` (all users when `name` is omitted) |
| `users.sshKeys` | `{name}` | `Key[]`; own keys at user level, other users need the root bridge (`needs_admin` otherwise) |

`User`:
```json
{"name":"mario","uid":1001,"gid":1001,"primaryGroup":"mario","fullName":"Mario Rossi","home":"/home/mario",
 "shell":"/bin/bash","groups":["wheel","docker"],"isAdmin":true,
 "locked":false,"passwordState":"ok","passwordChanged":41,"mustChange":false,"expired":false,
 "noLoginShell":false,"lastLogin":{"at":1790871731,"from":"192.168.1.20","tty":"pts/1"},"neverLoggedIn":false}
```
- `groups` are the supplementary groups (primary group is `primaryGroup`). `isAdmin`: root or member (or primary
  group) of `wheel`/`sudo`/`admin`.
- `locked`, `passwordState`, `passwordChanged` (days since the change), `mustChange`, `expired` need `/etc/shadow`:
  with the user bridge `locked` is `null` and `passwordState` is `"unknown"`. The web app calls `users.list` with
  `admin:true` while administrator rights are unlocked. `passwordState`: `ok`, `locked` (usable password behind `!`),
  `disabled` (`*`, `!`, `!*`: no password can match), `empty` (no password required), `unknown`.
- `lastLogin` merges `lastlog2`, `lastlog` and `last -w --time-format iso` (newest wins); `at` is unix seconds.
  `neverLoggedIn` is true when a source reports no login. When no source works `lastLoginKnown` is false.

`Group`: `{"name":"wheel","gid":998,"members":["mario"],"primaryMembers":[],"system":true,"description":"Can use sudo"}`.
`system` = gid < GID_MIN. `description` is English text for well-known groups, empty otherwise. `primaryMembers` are
users whose primary gid is this group.

`Key`: `{"type":"ssh-ed25519","comment":"laptop","fingerprint":"SHA256:...","options":"no-pty","key":"ssh-ed25519 AAAA...","line":3}`.
Unsupported or malformed lines are skipped. Types: ssh-rsa, ssh-dss, ssh-ed25519, ecdsa-sha2-nistp256/384/521,
sk-ssh-ed25519@openssh.com, sk-ecdsa-sha2-nistp256@openssh.com.

`Session` (from `loginctl list-sessions` + `show-session`, manager sessions hidden):
`{"id":"2","user":"mario","uid":1001,"type":"tty","class":"user","tty":"pts/1","remote":true,"remoteHost":"10.0.0.5","service":"sshd","seat":"","state":"active","since":1790871726}`.

## SSH key writes (user level, own keys; other users need the root bridge)

| Method | Params | Result |
|---|---|---|
| `users.addSshKey` | `{name, key}` | the parsed `Key`. One line; `invalid` when it does not parse, `conflict` when already present |
| `users.removeSshKey` | `{name, fingerprint}` | `{ok:true}`; removes every line holding that key, other lines are kept byte for byte; `not_found` if absent |

The file is replaced atomically with mode 0600 (`~/.ssh` created 0700 when missing). In the root bridge the file
system identity is switched to the owner (`setfsuid/setfsgid`) while reading and writing, and symlinks are never
followed, so files stay owned by the user and a user-planted symlink cannot redirect a root write.

## Admin methods

| Method | Params | Result / notes |
|---|---|---|
| `users.create` | `{name, fullName, shell, password, admin, groups[], createHome}` | `{ok, name}`. `useradd -c -s (-m|-M) (-G ...) name`, then `chpasswd`; if the password is rejected the user is removed again. `admin` adds the system's admin group (wheel, else sudo, else admin). `shell` defaults to /bin/bash. `createHome` defaults to true. `conflict` if the user or a group of that name exists |
| `users.modify` | `{name, fullName?, shell?, groups?: {add[], remove[]}}` | `{ok}`. The primary group cannot be removed |
| `users.setPassword` | `{name, password, mustChange?}` | `{ok}`. A locked account stays locked; `mustChange` runs `chage -d 0` |
| `users.lock` / `users.unlock` | `{name}` | `{ok}` (`usermod -L/-U`). Locking yourself is `forbidden` |
| `users.delete` | `{name, removeHome}` | `{ok}`. Refuses root, yourself (the user the root bridge works for) and system accounts |
| `users.setAdmin` | `{name, admin}` | `{ok}`. Adds/removes the user from the admin group(s). Refuses root and removing the last administrator (`conflict`) |
| `users.terminateSession` | `{id}` | `{ok}` (`loginctl terminate-session`) |
| `groups.create` | `{name}` | `{ok, name}` |
| `groups.delete` | `{name}` | `{ok}`. Refuses system groups and groups that are someone's primary group |
| `groups.addMember` / `groups.removeMember` | `{group, user}` | `{ok}`, idempotent. The root group and system accounts are refused |

Error codes used: `invalid` (bad name/shell/password/key), `not_found`, `conflict`, `forbidden`, `needs_admin`,
`unavailable` (a tool is not installed), `internal` (tool failure, message carries its stderr).

## Windows backend

On Windows (`users_windows.go`, `winaccounts.go`) local accounts are handled through PowerShell's LocalAccounts
cmdlets. Scripts are fixed text run with `-EncodedCommand`; all data (names, passwords) is ASCII-escaped JSON on
stdin, never on a command line or interpolated into a script.

- `uid`/`gid` carry the RID of the account/group SID; the full SID is in the extra `sid` field. `home` is the
  profile path (empty until the profile exists), `shell` is empty and `noLoginShell` is true. `people` are
  RID 500 and RID >= 1000; other built-ins are listed under `system`. `isAdmin` means member of Administrators.
- `users.lock`/`unlock` disable/enable the account (`passwordState: "disabled"`). `users.setPassword` with
  `mustChange` sets "must change at next logon". `users.modify` also accepts `description`.
- `users.sessions`, `sshKeys`, `addSshKey`, `removeSshKey`, `terminateSession` and changing `shell` return
  `unavailable`. Access denied from PowerShell maps to `needs_admin`.
- Names follow Windows rules (users up to 20 chars, no `" / \ [ ] : ; | = , + * ? < > @`), case-insensitive.
