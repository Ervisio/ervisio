# Packaging

LinuxAdmin installs in a versioned layout so it can update itself (`docs/RELEASING.md`):

| File | Installed as |
|---|---|
| `server/bin/linuxadmind`, `server/bin/linuxadmin-bridge` | `/usr/lib/linuxadmin/versions/<v>/bin/` |
| `web/dist/` | `/usr/lib/linuxadmin/versions/<v>/web/` |
| first-party plugins | `/usr/lib/linuxadmin/versions/<v>/plugins/<id>/` |
| — | `/usr/lib/linuxadmin/current -> versions/<v>` (and `previous`, kept for rollback) |
| — | `/usr/bin/linuxadmind -> /usr/lib/linuxadmin/current/bin/linuxadmind` |
| `linuxadmin.service` | `/etc/systemd/system/linuxadmin.service` |
| `pam.d/linuxadmin` | `/etc/pam.d/linuxadmin` |

Scripts: `install.sh` installs a release folder (an extracted release archive, or the staging folder made by
`install-dev.sh` from a local build) into that layout; `build-release.sh VERSION ARCH OUTDIR` packs a built tree into
`linuxadmin-VERSION-linux-ARCH.tar.gz` (used by the release workflow and `make dist`). Self-update state lives in
`/var/lib/linuxadmin/updates/`.

Runtime paths: config `/etc/linuxadmin/linuxadmin.conf` (optional; defaults apply when missing),
self-signed certificate in `/etc/linuxadmin/tls/` (generated on first start), plugins installed
from the UI in `/var/lib/linuxadmin/plugins/`.

Without `/etc/pam.d/linuxadmin` the daemon falls back to the `login` PAM service. The PAM file is
written for Arch (`system-login`); adapt the includes for other distributions. It needs `auth`,
`account` and `session` lines: each user bridge runs inside a PAM session opened by
`linuxadmind --pam-session-helper` (as root), so `pam_limits`, `pam_loginuid` and `pam_systemd` apply.
Accounts whose shell is `nologin`/`false`, a restricted shell or not listed in `/etc/shells` cannot sign in. Admin unlock uses
the system `sudo`: users must be allowed by sudoers to run the bridge (`%wheel ALL=(ALL) ALL`).

Build: `make build` (web + server). Set `VERSION=` to stamp the version (`linuxadmind --version`). A PKGBUILD will follow;
it should install the same versioned layout. (A flat install that runs a build with the updater is migrated to the
versioned layout by its first self-update, which a package manager would not know about.)
