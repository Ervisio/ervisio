# Changelog

## [0.2.0] - 2026-10-02

### Added
- Sign in with an SSH key. The browser decrypts the private key with its passphrase and signs a one-time challenge; the key never leaves the browser. The server checks it against the user's `~/.ssh/authorized_keys` (honouring `from=` and `expiry-time=`, refusing `command=` keys). `auth.ssh_keys` turns it off.
- Choose who may sign in: `auth.allow_users`, `auth.allow_groups` and `auth.admins_only`, also in Settings › Sign-in & security. Removing someone ends their sessions within a minute.
- `tls.mode = "http"` serves plain HTTP on a loopback address, for a reverse proxy on the same machine.
- `linuxadmind --check-config [file]` validates a configuration file.
- The installer asks for the port (and finds another one when 9090 is taken, for example by Cockpit), who may sign in, root sign-in, the admin unlock time and the listen address, and shows a summary before applying. New flags such as `--port`, `--admins-only`, `--allow-users`, `--behind-proxy`, `--reconfigure`.
- The installer can set up Caddy, installed natively or running in Docker: it points an existing `cockpit` site at LinuxAdmin or adds a new site for a domain you choose, after a backup and validation, then reloads Caddy.
- Docker plugin 2.0, a full container manager: container pages with live logs, stats, a shell and settings; compose stacks in `/opt/stacks` with an editor, diff and deploy output, plus stacks started elsewhere; a creation wizard that also edits containers by recreating them; app templates from our catalog and from Portainer-format lists; images with update checks, volumes, networks and registries; clean-up; auto-update through Watchtower; alerts.
- Plugin SDK 3: plugins can call an HTTP API on a unix socket within declared method and path rules, open terminals for declared commands, use folders that need admin rights, and open links in a new tab.

### Changed
- Release binaries are built against glibc 2.17, so they also run on Amazon Linux 2, RHEL 8, Debian 10 and Ubuntu 20.04.
- When the system's openssl cannot verify ed25519 signatures (OpenSSL older than 3), the installer verifies the release with an embedded Python verifier.
- Self-update works with systemd older than 236.
- Admin unlock first tries without a password, for `sudo` rules with NOPASSWD.
- When Caddy runs in Docker, LinuxAdmin listens only on the Docker network's gateway address instead of every interface.

## [0.1.2] - 2026-10-01

### Fixed
- Sign-in failed with "cross-origin request refused" when the console was opened through a reverse proxy or under a different host name than the one the daemon saw. The origin check now accepts the host forwarded by a trusted proxy (loopback by default) and treats default ports as equivalent.

### Added
- `web.allowed_origins` and `web.trusted_proxies` configuration keys. Refused origins are logged with a hint.
- Rate limits and logs use the browser's address from `X-Forwarded-For` when the request comes from a trusted proxy.

## [0.1.1] - 2026-10-01

### Added
- MIT license.
- One-line installer: `curl -fsSL https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh | sudo sh`. Verifies the release signature with openssl, picks the PAM file for the distribution, installs sudo if missing, can open the port in ufw or firewalld, prints the addresses and the certificate fingerprint. `--version`, `--prerelease`, `--uninstall [--purge]`, `--dry-run`, `--yes`.
- `.deb` and `.rpm` packages for x86-64 and ARM64 on the release page, covered by the signed `SHA256SUMS`.
- PKGBUILDs for the AUR (`linuxadmin-bin`, `linuxadmin`).
- PAM files for Debian/Ubuntu, Fedora/RHEL and openSUSE next to the Arch one.

### Changed
- A LinuxAdmin installed by a package manager does not update itself: Settings › About says which package manager installs updates, and automatic installs are off.
- The daemon finds the bridge in `/usr/lib/linuxadmin` when it is not next to the daemon (package layout).

## [0.1.0] - 2026-10-01

First public release. Early software: tested by hand on Arch Linux, other distributions are untested.

### Added
- Sign-in with Linux accounts through PAM. Root login is off by default.
- Administrator rights through `sudo`, with a configurable unlock time (including until sign-out).
- Overview: live metrics, activity chart, alerts, custom action buttons, editable widget layout, refresh rate from 1 second to 10 minutes.
- Terminal: persistent sessions (local, root, SSH), split panes, command palette, focus mode, saved commands.
- Files: tabs, split view, grid and list, previews, permissions editor, trash, uploads and downloads.
- Logs: journal, kernel, services and log files, level filters, histogram, live follow, file watchers with notifications.
- Services: systemd units by state and purpose, start/stop/restart/enable, unit overrides, dependencies.
- Software: updates, search, installed packages and history for pacman (with AUR listing), apt, dnf, zypper and Flatpak; scheduled updates.
- Users: accounts, groups, admin rights, passwords, SSH keys and login sessions.
- Plugins: sandboxed plugin pages and widgets, signed plugins, Docker example plugin.
- Settings: nine themes, custom themes, colour modes (per section, distro colour, monochrome), English and Italian.
- Self-update from signed GitHub releases with automatic rollback.
