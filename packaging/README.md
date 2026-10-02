# Packaging

Ervisio is installed in one of two layouts (details and publishing: `docs/PACKAGING.md`):

- **Versioned**, by `install.sh` (repository root) and `install-dev.sh`. Ervisio updates itself
  (`docs/RELEASING.md`).
- **Flat**, by the `.deb`, `.rpm` and AUR packages. The package manager installs updates; the marker
  `/usr/lib/ervisio/managed` turns self-update off.

| From the release / build | Versioned layout | Flat layout (packages) |
|---|---|---|
| `bin/ervisiod` | `/usr/lib/ervisio/versions/<v>/bin/`, `/usr/bin/ervisiod -> …/current/bin/ervisiod` | `/usr/bin/ervisiod` |
| `bin/ervisio-bridge` | `/usr/lib/ervisio/versions/<v>/bin/` | `/usr/lib/ervisio/ervisio-bridge` |
| `web/` | `/usr/lib/ervisio/versions/<v>/web/` | `/usr/share/ervisio/web/` |
| `plugins/<id>` | `/usr/lib/ervisio/versions/<v>/plugins/<id>/` | `/usr/share/ervisio/plugins/<id>/` |
| `ervisio.service` | `/etc/systemd/system/ervisio.service` | `/usr/lib/systemd/system/ervisio.service` |
| `pam.d/ervisio.<family>` | `/etc/pam.d/ervisio` | `/etc/pam.d/ervisio` |
| `package-service.sh` | — | `/usr/lib/ervisio/package-service` |
| — | `current`, `previous` symlinks (rollback) | `/usr/lib/ervisio/managed` (`apt`, `dnf`, `zypper`, `pacman`) |

Files here:

| File | What it does |
|---|---|
| `../install.sh` | The installer: downloads a release, verifies `SHA256SUMS.sig` (ed25519: openssl 3, or a built-in Python verifier on old systems) and the sha256, installs the versioned layout, PAM file, unit, starts the service. `--from DIR` installs a release folder already on disk. `--uninstall [--purge]`, `--dry-run`, `--open-firewall`, and the configuration options and questions (port, listen address, root sign-in, admin unlock time, TLS, Caddy), see `docs/PACKAGING.md`. |
| `install.sh` | Shipped in each release archive: runs the archive's own `install.sh --from <archive folder>`. |
| `install-dev.sh` | Stages a local `make build` as a release folder and runs `install.sh --from` on it; `--remove` uninstalls. |
| `build-release.sh VERSION ARCH OUTDIR` | Packs a built tree into `ervisio-VERSION-linux-ARCH.tar.gz` (release workflow, `make dist`). |
| `build-packages.sh RELEASE_DIR ARCH OUTDIR` | Builds `ervisio_VERSION_ARCH.deb` and `ervisio-VERSION-1.RPMARCH.rpm` from an extracted release archive with nfpm. |
| `pkg/nfpm.yaml`, `pkg/scripts/` | nfpm configuration and the deb/rpm install scripts. |
| `package-service.sh` | Restarts or stops the service from package scripts; defers it when the package manager runs inside Ervisio. |
| `pam.d/ervisio.{arch,debian,fedora,suse}` | PAM service per distribution family. |
| `arch/ervisio-bin`, `arch/ervisio` | AUR packages (PKGBUILD, .SRCINFO, install script, copies of the PAM file and `package-service.sh`). |
| `arch/update-pkgbuild.sh VERSION` | Points both PKGBUILDs at a release: verified checksums, pkgver, `.SRCINFO`. |

Runtime paths, the same for every layout: configuration `/etc/ervisio/ervisio.conf` (optional; defaults apply
when missing), self-signed certificate in `/etc/ervisio/tls/` (generated on first start), plugins installed from
the UI in `/var/lib/ervisio/plugins/`, self-update state in `/var/lib/ervisio/updates/`.

Without `/etc/pam.d/ervisio` the daemon falls back to the `login` PAM service. The PAM file needs `auth`,
`account` and `session` lines: each user bridge runs inside a PAM session opened by `ervisiod
--pam-session-helper` (as root), so `pam_limits`, `pam_loginuid` and `pam_systemd` apply. The installers pick
`pam.d/ervisio.<family>` from `ID`/`ID_LIKE` in `/etc/os-release`: `arch` (system-login), `debian`
(`@include common-*`), `fedora` (password-auth, postlogin), `suse` (common-*). Accounts whose shell is
`nologin`/`false`, a restricted shell or not listed in `/etc/shells` cannot sign in. Admin unlock uses the system
`sudo`: users must be allowed by sudoers to run the bridge with their own password (`%wheel ALL=(ALL) ALL`; on
openSUSE install `sudo-policy-wheel-auth-self`, the default asks for root's password).

Build: `make build` (web + server). Set `VERSION=` to stamp the version (`ervisiod --version`).
