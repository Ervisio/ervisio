# Changelog

## [Unreleased]

### Added
- **Large transfers for plugins.** `sdk.api.download(name, req, filename)` (and `downloadCommand`) saves the response of a `GET` to a plugin's HTTP API, or a declared command's output, as a browser download: the bytes stream from the service to the disk with no memory use and no size limit. `sdk.api.upload(name, req, file, …)` sends a `File` as the body of a `POST` or `PUT` with progress, cancel and a size limit from the manifest (`capabilities.http[].maxUpload`, default 20 GiB), and can hand the response back as it arrives (a Docker build). Both go through a one-time, 60-second link bound to the session, with the same rules, admin unlock, origin checks and rate limits as `plugins.http`. See `docs/api/plugins.md`, "Large transfers".
- **Activity log.** The daemon records every mutating plugin call (commands, terminals, HTTP calls other than `GET`/`HEAD`, uploads, file writes, folders, removals), downloads, sign-ins, administrator unlocks, plugin installs and removals and settings changes: time, user, address, plugin, target and result. Request headers, bodies and secrets in arguments or query strings are never stored. It is an append-only JSON-lines file per day under `/var/lib/ervisio/audit/`, kept for `audit.retention_days` (default 90) and switched by `audit.enabled`. Settings › Activity log lists, filters and exports it as CSV or JSON (everyone sees their own entries, administrators everyone's), and plugins read their own with `sdk.audit.list()`.
- `sdk.saveFile(filename, data, mime?)` saves data a plugin already holds (a string, bytes or a Blob, up to 64 MiB) as a browser download, done by the app because the plugin frame is sandboxed and cannot download; the name is cleaned and each frame is limited to 10 files in 30 seconds.
- `capabilities.http[].maxUpload`, and the settings `audit.enabled` and `audit.retention_days`.

### Changed
- A plugin HTTP request body of up to 8 MiB (the default `maxBody`) now reaches the service through `plugins.http` and `plugins.httpStream`. The limits were 1 MiB for `/api/rpc` and 512 KiB for the WebSocket frame that opens a stream, and a body over the second dropped the connection. They are now 12 MiB for both, a body over 8 MiB is refused with a message that points to `sdk.api.upload`, and writing a 4 MiB file with `plugins.writeFile` works.

## [0.4.0] - 2026-10-02

### Changed
- **The Docker plugin moved to the marketplace.** It is no longer part of Ervisio: releases and packages ship no plugins. Its source is at [Ervisio/plugin-docker](https://github.com/Ervisio/plugin-docker), and it is published, signed by the Ervisio team, through the registry [Ervisio/plugins](https://github.com/Ervisio/plugins). A machine that used it keeps it: on the first start after the update, on a host with a Docker socket where the plugin was not switched off, the daemon installs the marketplace version into `/var/lib/ervisio/plugins` (catalog signature, checksum and plugin signature are all checked; it retries every hour while offline). Until then, Plugins › Installed shows a "Docker moved to the marketplace" card with an Install button.
- Plugins › Browse reads the marketplace catalog by default (`plugins.catalog_url`, `https://ervisio.github.io/plugins/catalog.json`). A remote catalog must be signed (`catalog.sig`, ed25519, by the team key or `plugins.catalog_key`); an unsigned or tampered one is ignored with a warning. `/etc/ervisio/plugins-catalog.url` still overrides the address, `plugins.catalog_url = ""` turns the remote catalog off. Updates of installed plugins are offered from the remote catalog too.
- The plugin SDK documentation moved to [Ervisio/plugin-sdk](https://github.com/Ervisio/plugin-sdk), with the TypeScript types, the Vite build preset and a plugin template.

### Added
- `plugins.catalog_url` and `plugins.catalog_key` settings.
- `ervisiod --install-plugin ID` installs a plugin from the signed catalog; `ervisiod --skip-moved-plugins` keeps the daemon from adding the Docker plugin by itself.
- The installer offers the Docker plugin when Docker is installed (`--with-docker-plugin`, `--no-plugins`).
- `plugin-sign -catalog catalog.json` signs and verifies catalogs.
- Logo files in `docs/brand/pack` (mark, logo with name, monochrome, app icons, SVG).

## [0.3.0] - 2026-10-02

The first version named Ervisio.

### Changed
- **LinuxAdmin is now Ervisio.** The repository moves to `ervisio/ervisio`; the binaries are `ervisiod` and `ervisio-bridge`, the unit `ervisio.service`, the PAM service `ervisio`, and the folders `/etc/ervisio`, `/usr/lib/ervisio`, `/var/lib/ervisio`, `/usr/share/ervisio` and `~/.config/ervisio`. Release archives are `ervisio-<version>-linux-<arch>.tar.gz`, the packages `ervisio` (`.deb`, `.rpm`) and `ervisio-bin` / `ervisio` (AUR). The one-line installer is `curl -fsSL https://raw.githubusercontent.com/ervisio/ervisio/main/install.sh | sudo sh`.
- The session cookie is `ervisio_session` and POSTs send `X-Requested-With: ervisio` (`linuxadmin` is still accepted for this release). SSH key sign-in signs `ervisio-ssh-auth-v1` challenges.
- Docker plugin 2.0.1: settings in `~/.config/ervisio/plugins/docker`, auto-update container `ervisio-watchtower` (an existing `linuxadmin-watchtower` is picked up and replaced).

### Added
- Existing LinuxAdmin installations move to Ervisio by themselves. Updating from LinuxAdmin's Settings › About installs Ervisio, copies the configuration, TLS certificate, installed plugins, update state, PAM file, unit drop-ins and scheduled update, switches `linuxadmin.service` to `ervisio.service` on the same port, and goes back to LinuxAdmin if Ervisio does not answer. `install.sh` and the packages (which replace `linuxadmin`) do the same. Users' `~/.config/linuxadmin` and the browser's settings are carried over at the next sign-in. LinuxAdmin's files are kept for a rollback until `ervisiod --remove-legacy [--purge]` removes them. See `docs/RELEASING.md`, "Rename transition".
- Every release also publishes `linuxadmin-<version>-linux-<arch>.tar.gz`, the archive LinuxAdmin consoles update from.
- `ervisiod --migrate-legacy` and `ervisiod --remove-legacy [--purge]`.
- The Ervisio logo: favicon, sign-in screen, Settings › About and README.

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
