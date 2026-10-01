# Packaging

| File | Installed as |
|---|---|
| `linuxadmin.service` | `/usr/lib/systemd/system/linuxadmin.service` |
| `pam.d/linuxadmin` | `/etc/pam.d/linuxadmin` |
| `server/bin/linuxadmind` | `/usr/bin/linuxadmind` |
| `server/bin/linuxadmin-bridge` | `/usr/lib/linuxadmin/linuxadmin-bridge` |
| `web/dist/` | `/usr/share/linuxadmin/web/` |
| first-party plugins | `/usr/share/linuxadmin/plugins/<id>/` |

Runtime paths: config `/etc/linuxadmin/linuxadmin.conf` (optional; defaults apply when missing),
self-signed certificate in `/etc/linuxadmin/tls/` (generated on first start), plugins installed
from the UI in `/var/lib/linuxadmin/plugins/`.

Without `/etc/pam.d/linuxadmin` the daemon falls back to the `login` PAM service. The PAM file is
written for Arch (`system-login`); adapt the includes for other distributions. Admin unlock uses
the system `sudo`: users must be allowed by sudoers to run the bridge (`%wheel ALL=(ALL) ALL`).

Build: `make build` (web + server). Set `VERSION=` to stamp the version. A PKGBUILD will follow.
