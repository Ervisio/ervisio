# Packaging
Purpose: build, install and migrate Ervisio on Linux (release archive, install.sh, deb/rpm/AUR) and Windows, including the LinuxAdmin rename path.

## Entry points and public contracts
- `install.sh` (repo root, ~2600 lines, POSIX sh): downloads/verifies a release (`RELEASE_KEY_PEM`), installs versioned layout; `--from DIR`, `--uninstall [--purge]`, `--dry-run`, `--version`; `refuse_managed` blocks install/uninstall on packaged hosts.
- `packaging/install.sh`: shipped in the archive; runs the archive's own `install.sh --from <folder>`; needs `VERSION` file.
- `packaging/install-dev.sh`: stages `server/bin` + `web/dist` as a release folder, `--from`; `--remove` uninstalls. Version folder name = `ervisiod --version`.
- `packaging/build-release.sh VERSION ARCH OUT`: writes `ervisio-V-linux-A.tar.gz` and compat `linuxadmin-V-linux-A.tar.gz`.
- `packaging/build-compat.sh VERSION ARCH`: builds binaries in manylinux2014 (glibc 2.17) via docker; rejects newer glibc symbols.
- `packaging/build-packages.sh RELEASE_DIR ARCH OUT`: nfpm deb/rpm from an extracted archive; config `packaging/pkg/nfpm.yaml`, scripts `packaging/pkg/scripts/{deb,rpm}-*`.
- `packaging/package-service.sh`: restart/stop from package scripts; defers when the package manager runs under ervisiod/bridge (walks `/proc` parents).
- `packaging/ervisio.service`, `packaging/pam.d/ervisio.{arch,debian,fedora,suse}`; Arch: `packaging/arch/{ervisio,ervisio-bin}/PKGBUILD`, `arch/update-pkgbuild.sh X.Y.Z`.
- Windows: `packaging/windows/install.ps1` (service `Ervisio`, `-Uninstall`, `-PurgeData`), `ervisio.wxs` (optional WiX MSI), root `install-windows.ps1`.
- `server/internal/legacy`: `Import`, `ImportOnStart`, `Transition`, `StartTransition`, `RunMigrate`, `RunHelper`, `MigrateUserDir`; flags `--migrate-legacy`, `--remove-legacy`, `--transition` wired in `server/cmd/ervisiod/main.go`.

## Flow
- Two layouts. Versioned (install.sh): `/usr/lib/ervisio/versions/<v>/{bin,web,plugins}`, `current`/`previous` symlinks, `/usr/bin/ervisiod -> current/bin`, unit in `/etc/systemd/system`. Flat (deb/rpm/AUR): `/usr/bin/ervisiod`, `/usr/lib/ervisio/ervisio-bridge`, `/usr/share/ervisio/web`, unit in `/usr/lib/systemd/system`.
- Packages write `/usr/lib/ervisio/managed` (`apt`/`dnf`/`zypper`/`pacman`); that turns self-update off (see `map/updates.md`).
- deb-postinst: removes a prior versioned install (`versions`, `current`, `previous`, `/var/lib/ervisio/updates`) and installer-written unit (first-line header), runs `ervisiod --migrate-legacy` if linuxadmin dirs exist, enables/starts or restarts via `package-service`, then `--remove-legacy` if safe.
- Legacy move copies, never deletes, until Remove: `/etc/linuxadmin`->`/etc/ervisio`, `/var/lib/linuxadmin`->`/var/lib/ervisio`, PAM file, unit drop-ins, update timer, `~/.config/linuxadmin` (per-user, by the user's bridge). Entry paths: LinuxAdmin self-update (compat archive -> new binary runs Transition), install.sh/packages, or daemon start (`ImportOnStart` when `/etc/ervisio` missing; `StartTransition` from a LinuxAdmin layout).
- Release: workflow builds archives, then packages from the archive itself (same files), see `map/updates.md`.

## Invariants
- Archive holds only regular files and dirs (updater refuses others); `plugins/` is empty since 0.4.0 (marketplace installs to `/var/lib/ervisio/plugins`).
- Archive version must match `^x.y.z(-pre)?$`, no leading `v`; binaries must report exactly VERSION (checked when host arch = ARCH).
- File names avoid `~` so `SHA256SUMS` parses everywhere.
- Unit has `Conflicts=linuxadmin.service` (same port); no sandboxing options because bridges run user commands and sudo.
- Installer-written units start with `legacy.InstallerUnitHeader`; packages and uninstall recognise it.
- PAM file needs auth/account/session; `ervisiod --pam-session-helper` runs the session as root. Missing file falls back to `login`.
- Go cgo build needs libpam headers; Windows is pure Go (`CGO_ENABLED=0`).

## Gotchas
- The compat archive keeps LinuxAdmin names (`linuxadmind`, `linuxadmin-bridge`, `linuxadmin.service`, `pam.d/linuxadmin.*`) on purpose; old consoles' updater asks for exactly that name.
- Signature prefix `linuxadmin-release-v1\n` is historical in `update/sign.go` and `install.sh`; change only with key rotation.
- `install.sh` embeds the release key; `TestInstallScriptReleaseKey` fails if it drifts from `ReleasePublicKey`. One-line install fetches `install.sh` from `main`.
- Debian/RPM post scripts duplicate the legacy logic in shell; keep them aligned with `legacy` and `install.sh` (`--remove-legacy` only when `/usr/lib/linuxadmin/managed` is absent and linuxadmin.service inactive).
- AUR: stable releases only; `update-pkgbuild.sh` verifies the signature (needs go) and refreshes local copies of `ervisio.pam` and `package-service.sh` in each PKGBUILD dir; repo PKGBUILDs are committed by hand.
- Windows has no self-update (`ErrWindows`): rerun `install.ps1`. MSI is not built in CI; binaries are not Authenticode-signed.
- Makefile `LDFLAGS` sets `brand.Version`; without VERSION it is `git describe`, which `install-dev.sh` validates as a folder name.

## Commands and references
- `make build`, `make build-server`, `make build-windows`, `make test-server`, `make dist VERSION=X.Y.Z` (host arch, unsigned, into `dist/`), `make clean`.
- `sudo ./packaging/install-dev.sh [--dry-run|--remove]`; `sh install.sh --dry-run --version X.Y.Z`.
- Docs: `docs/PACKAGING.md` (layouts, managed installs, AUR/COPR/PPA/OBS, Windows), `packaging/README.md` (file table, PAM families), `docs/RELEASING.md` "Rename transition".
- Tests: `server/internal/legacy/legacy_unix_test.go` (fake root via `Paths.Root`).
Related: map/updates.md, map/server.md
