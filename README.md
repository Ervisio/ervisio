# LinuxAdmin

A web console for managing a Linux server from the browser, in the spirit of Cockpit. You sign in with your Linux account and get an overview, a terminal, a file manager, logs, services, software updates, user management and a plugin system. The name is a working name.

![Overview](docs/screenshots/overview.png)

## Status

LinuxAdmin is early software. It runs and I use it on my own machine, but it has had little exposure.

- Tested by hand on Arch Linux: PAM sign-in, admin unlock through `sudo`, every section in the screenshots below.
- Admin actions (package transactions, user and group changes, file operations as root, config changes) are covered mostly by unit tests, with limited manual testing.
- The package manager backends for apt, dnf, zypper and Flatpak exist and have unit tests. I have not run them on real Debian, Fedora or openSUSE machines.
- Installing, sign-in and admin unlock were tested in containers on Debian 12, Ubuntu 24.04, Fedora 44 and openSUSE Tumbleweed, with the install script and with the `.deb` and `.rpm` packages. The sections themselves were not tested there.
- Releases are published on GitHub with signed archives and, from 0.1.1, `.deb` and `.rpm` packages. The AUR packages are written but not published yet.
- Self-update from GitHub releases is implemented (Settings > About) but has not yet been exercised on a live install.
- A security review of the code is in [docs/SECURITY-REVIEW.md](docs/SECURITY-REVIEW.md). It lists each finding and how it was fixed. It is a self review, not an external audit.

## Features

### Overview

CPU, memory, disk and network at a glance, an activity chart, a list of failed units and pending problems, and a card with machine details. The dashboard is customizable: widgets can be added, resized and reordered, and you can define your own command shortcuts ("My actions").

### Terminal

Shell sessions in the browser, backed by a real PTY running as the signed-in user. Sessions can be split and started on saved hosts. A bar of one-click commands (disk usage, memory, failed services, follow system log) and your own saved commands sits under the terminal.

![Terminal](docs/screenshots/terminal.png)

### Files

A file manager with places (home, root, recent, starred, trash), bookmarks, tabs, a split view, grid and list layouts, search, upload and download. It works as your user. When you unlock administrator rights it can work on system files as root. SFTP remotes are planned.

![Files](docs/screenshots/files.png)

### Logs

One view over the systemd journal, the kernel log, individual services and plain log files such as `/var/log/pacman.log`. Filter by level and by text, pick a time range, follow live, and see a histogram of entries over time.

![Logs](docs/screenshots/logs.png)

### Services

All systemd units grouped by state and by purpose (web, containers, system), as a table or as cards. The details panel for a unit shows status, process ID and memory, a live journal tab, the unit file and its dependencies. You can start, stop, restart, reload, enable at boot and mask units.

![Services](docs/screenshots/services.png)

### Software

Updates, search, installed packages and history for the system package manager, plus Flatpak when present. Updates can run now or be scheduled. On Arch, AUR packages are listed and searched, and removed through pacman. Installing and upgrading AUR packages stays in the terminal on purpose, because AUR builds must not run as root.

| Manager | Detected by |
|---|---|
| pacman (and AUR through `yay` or `paru`) | `pacman` in PATH |
| apt | `apt-get` and `dpkg-query` |
| dnf | `dnf` and `rpm` |
| zypper | `zypper` and `rpm` |
| Flatpak | `flatpak` |

![Software](docs/screenshots/software.png)

### Users

People, system accounts and groups, as cards or as a table. Create, modify and delete accounts, set passwords, grant or remove admin rights, lock and unlock accounts, manage SSH keys and end user sessions. Some details, such as lock state and password age, need administrator rights to read.

![Users](docs/screenshots/users.png)

### Plugins

Plugins add pages, dashboard widgets and terminal snippets. The repository ships one first-party plugin for Docker. The Plugins section lists installed plugins, shows what each one is allowed to do before you enable it, and has tabs for updates, a catalog, security policy and developer mode. See [Plugins](#plugins) below.

![Plugins](docs/screenshots/plugins.png)

### Settings and themes

Nine built-in themes (OLED, OLED Mono, Midnight, Graphite, Fjord, Forest, High contrast, Daylight, Linen), custom themes, English and Italian, notification preferences, and server settings (sign-in and security, web server and TLS, plugin policy, hosts). Server settings need administrator rights. Preferences are stored per user on the server, so they follow you between browsers.

![Settings](docs/screenshots/settings.png)

### Sign-in and small screens

The sign-in page shows the host name and distribution. The interface adapts to phone widths with a bottom navigation bar.

![Sign-in](docs/screenshots/sign-in.png)

<img src="docs/screenshots/mobile-overview.png" alt="Overview on a phone" width="300">

## How it works

Two binaries. The daemon runs once per machine; each signed-in user gets their own bridge process.

```
browser --HTTPS / WebSocket--> linuxadmind (root, one per machine)
                                 |  PAM sign-in, sessions, routing, static files
                                 |
                                 +-- stdio JSON --> linuxadmin-bridge  (runs as the signed-in user)
                                 |
                                 +-- stdio JSON --> linuxadmin-bridge --admin
                                                    (root, started with `sudo -S`
                                                     only after the user unlocks)
```

- `linuxadmind` serves the web app on port 9090, authenticates with PAM, keeps sessions and routes requests. It does not change the system itself.
- `linuxadmin-bridge` does the system work: systemd, files, journal, packages, users. The daemon starts one bridge per session with the user's uid, gid and groups.
- Administrator rights use `sudo`. When you unlock, the daemon runs `sudo -S -- linuxadmin-bridge --admin` as you and writes your password to its standard input. The sudoers policy decides. If you may not use sudo, unlocking fails. The root bridge stops after 5 minutes without admin calls by default (configurable, including "until sign-out"), or when you lock or sign out.

Details are in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and the per-section API notes in [docs/api/](docs/api/).

## Security model

- **Authentication** is PAM, through the service `linuxadmin` (falls back to `login`). Accounts with a `nologin` or `false` shell, a restricted shell, or a shell missing from `/etc/shells` cannot sign in. Failed attempts are rate limited per client address.
- **Authorization** is `sudo`. LinuxAdmin has no user database and no role system of its own. A user can do as root what sudoers lets them do, and nothing more.
- **Root login is off** unless `allow_root = true` is set in the configuration.
- **Sessions** use 256-bit random tokens stored only as SHA-256 hashes, an `HttpOnly`, `SameSite=Strict` cookie, an idle timeout and an absolute lifetime. Accounts are re-checked periodically, so removing a user from a group or locking an account ends the session.
- **CSRF**: every POST needs the header `X-Requested-With: linuxadmin`, and a present `Origin` must match the host.
- **Plugins** run in a sandboxed iframe with an opaque origin and a strict content security policy. They have no cookies and no direct API access. A broker enforces the capabilities declared in the plugin manifest. Plugins must be signed with the project key, or `plugins.allow_unsigned` must be turned on. See [docs/PLUGIN-SIGNING.md](docs/PLUGIN-SIGNING.md).
- **Findings and fixes** from the self review are listed in [docs/SECURITY-REVIEW.md](docs/SECURITY-REVIEW.md).

Run it only on networks you trust until it has had more review. A binary that runs as root and executes commands for browser users is a high-value target.

## Requirements

To run:

- Linux with systemd, PAM and `sudo`, x86-64 or ARM64, glibc 2.17 or newer
- A user in a sudoers rule that allows running the bridge (on Arch, `%wheel ALL=(ALL) ALL`)

To build:

- Go 1.24 or newer
- gcc and the PAM headers (`pam_appl.h`), because the PAM wrapper uses cgo
- Node.js and npm for the web app

## Install

LinuxAdmin needs Linux with systemd on x86-64 or ARM64, and glibc 2.17 or newer (Amazon Linux 2, RHEL 8, Debian 10, Ubuntu 20.04, or newer).

### One-line install

```sh
curl -fsSL https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh | sudo sh
```

The script downloads the latest release from GitHub, checks its signature and checksum, installs it in `/usr/lib/linuxadmin`, writes the PAM file for your distribution and the systemd unit, installs `sudo` if it is missing, and starts the service. At the end it prints the addresses to open and the SHA-256 fingerprint of the certificate, so you can compare it with what the browser shows before you accept the warning. LinuxAdmin installed this way updates itself from Settings > About. Running the script again repairs or upgrades the installation and keeps your configuration.

To read the script before running it:

```sh
curl -fsSLO https://raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh
less install.sh
sudo sh install.sh
```

**Questions.** On a first install the script asks a few things and shows the default in brackets; press Enter to accept it. The questions are read from the terminal, so they also work when the script is piped into `sh`. With `--yes`, or when there is no terminal (a provisioning script), nothing is asked and every default is used.

1. Port (default 9090). If something already listens there, the script says what (Cockpit's `cockpit.socket` uses 9090 too) and asks for another port, suggesting the next free one.
2. Caddy, if it finds one (see below), or whether LinuxAdmin sits behind another reverse proxy.
3. Who can reach LinuxAdmin: all interfaces (default) or only this machine (`127.0.0.1`). A reverse proxy decides this by itself.
4. TLS: a self-signed certificate (default) or your own certificate and key files.
5. Allow signing in as root (default: no).
6. How long administrator rights stay unlocked: 5 minutes (default), 15 minutes, 1 hour, or until sign-out.
7. Start at boot and start now (default: yes to both).

Then it prints a summary and asks for confirmation before it changes anything, and writes `/etc/linuxadmin/linuxadmin.conf` with a comment for each setting. Any local account with a real login shell can sign in; there is no list of allowed users in the configuration yet. Administrator rights come from sudo (the `wheel` or `sudo` group).

On a machine that is already configured, the existing file is kept as it is. `--reconfigure` (or any configuration option below) changes the keys the questions cover, keeps the rest of the file, and saves a backup next to it first.

**Caddy.** If the script finds Caddy, either installed on this machine (`caddy` or `caddy.service`, with `/etc/caddy/Caddyfile`) or in a running Docker container whose image name contains `caddy`, it asks whether to put LinuxAdmin behind it:

- If the Caddyfile has a site whose address contains `cockpit` (for example `cockpit.example.org { reverse_proxy localhost:9090 }`), it offers to point that site to LinuxAdmin.
- Otherwise it asks for a (sub)domain, suggesting `linuxadmin.` plus the domain of your other sites, and adds a new site block at the end of the Caddyfile.
- The block is `reverse_proxy https://127.0.0.1:PORT` with `transport http { tls_insecure_skip_verify }`. LinuxAdmin only serves HTTPS, so Caddy connects with HTTPS and skips the check of the self-signed certificate on that local hop. Browsers see Caddy's own certificate. WebSockets work through `reverse_proxy` without extra settings. LinuxAdmin then listens on `127.0.0.1` only.
- Caddy in Docker cannot reach `127.0.0.1` of the host. The script then uses `host.docker.internal` if the container has it in `extra_hosts`, otherwise the gateway address of the container's network (for example `172.17.0.1`), and LinuxAdmin listens on all interfaces so the container can connect. With `network_mode: host` the loopback address is used. Allow the Docker subnet in your firewall if it blocks container-to-host traffic (ufw does by default); the script prints the command. The container's Caddyfile has to be a bind mount from this machine, so the script can edit it.
- `web.allowed_origins` is set to `https://<domain>` and `web.trusted_proxies` to loopback (plus the Docker subnet for Caddy in Docker).
- Before editing, the script validates the Caddyfile, saves a copy as `Caddyfile.linuxadmin-backup-<date>` next to it, edits it in place, validates again and restores the copy if Caddy rejects the result. Then it reloads Caddy (`systemctl reload caddy`, or `docker exec ... caddy reload`). If the Caddyfile has something its small parser does not follow (a heredoc, a single-site file without braces, several `reverse_proxy` lines in the Cockpit block, unusual braces), nothing is edited and the block to paste is printed.
- With `--yes` Caddy is only changed when you pass `--caddy`.
- The domain has to point to this machine so Caddy can get a certificate.

Options (after `sh -s --` when piping, for example `curl ... | sudo sh -s -- --version 0.1.1`):

| Option | Effect |
|---|---|
| `--version X.Y.Z` | install that release instead of the latest stable one |
| `--prerelease` | install the newest release, pre-releases included |
| `--port N` | port to listen on (default 9090) |
| `--listen all\|local\|IP` | all interfaces (default), `127.0.0.1` only, or one address |
| `--allow-root`, `--no-allow-root` | allow signing in as root (default: no) |
| `--admin-unlock D` | `5m` (default), `15m`, `1h`, any time from `30s` to `24h`, or `signout` |
| `--tls-cert FILE --tls-key FILE` | use your own certificate instead of the self-signed one |
| `--behind-proxy` | another reverse proxy on this machine fronts LinuxAdmin: listen on `127.0.0.1` |
| `--origin URL` | add a browser origin to `web.allowed_origins` (repeatable) |
| `--trusted-proxy ADDR` | add an address or CIDR to `web.trusted_proxies` (repeatable) |
| `--caddy`, `--no-caddy` | set up, or never touch, a Caddy found on this machine or in Docker |
| `--domain NAME` | the domain Caddy serves LinuxAdmin on (implies `--caddy`; an existing site with that address is pointed to LinuxAdmin) |
| `--reconfigure` | ask the configuration questions again on an installed system |
| `--no-enable`, `--no-start` | do not enable at boot, do not start the service |
| `--open-firewall` | open the port in ufw or firewalld when one is active (otherwise the script only tells you the command) |
| `--dry-run` | show what would be done and change nothing |
| `--yes` | do not ask questions; every answer is the default |
| `--uninstall` | remove LinuxAdmin, keeping `/etc/linuxadmin` and installed plugins (`--purge` removes those too) |

The signature check uses OpenSSL 3 when the system has it. Older systems (Amazon Linux 2, CentOS 7: OpenSSL 1.0.2 or 1.1.1, which cannot verify Ed25519) use a small Python 2.7 or 3 verifier that the script carries; both of those systems have Python. It has to pass the RFC 8032 test vector before it is used. If neither is available the script stops; `--insecure-skip-signature` installs anyway, checking only the checksum.

### Packages

Each release has `.deb` packages (Debian 12 and newer, Ubuntu 22.04 and newer) and `.rpm` packages (Fedora, RHEL 9 and clones, openSUSE) for x86-64 and ARM64 on the [release page](https://github.com/Fonlogen/LinuxAdmin/releases), from version 0.1.1 on:

```sh
sudo apt install ./linuxadmin_0.1.1_amd64.deb
sudo dnf install ./linuxadmin-0.1.1-1.x86_64.rpm
sudo zypper install ./linuxadmin-0.1.1-1.x86_64.rpm
```

The package enables and starts the service. A packaged LinuxAdmin does not update itself: Settings > About says which package manager installs updates, and you install the next package the same way. The packages are listed in the signed `SHA256SUMS` of the release. There is no apt or dnf repository yet.

### Arch Linux (AUR)

Once published, `yay -S linuxadmin-bin` (prebuilt) or `yay -S linuxadmin` (built from source), then `sudo systemctl enable --now linuxadmin`. The PKGBUILDs are in [packaging/arch](packaging/arch). On Arch the `%wheel` rule in sudoers is commented out by default; enable it with `visudo` to get administrator rights.

### From source

```sh
git clone https://github.com/Fonlogen/LinuxAdmin.git
cd LinuxAdmin
make build                          # builds web/dist and server/bin/*
sudo ./packaging/install-dev.sh     # installs and starts the systemd service
```

`install-dev.sh` puts the local build into the same layout as `install.sh`. Read the scripts before you run them. To remove everything except the configuration, run `sudo ./packaging/install-dev.sh --remove`.

### After installing

Open `https://<host>:9090` and sign in with a Linux account. On first start the daemon generates a self-signed certificate in `/etc/linuxadmin/tls/`, so the browser shows a warning once. Accounts in the `wheel` group (`sudo` on Debian and Ubuntu) can unlock administrator rights with their own password.

Where things are installed, for each method, and how to publish more packages: [docs/PACKAGING.md](docs/PACKAGING.md) and [packaging/README.md](packaging/README.md).

## Configuration

The file is `/etc/linuxadmin/linuxadmin.conf` in TOML. It is optional, and missing keys use the defaults below. Most keys can also be changed in Settings with administrator rights, which keeps a `.bak` copy of the previous file.

```toml
listen = "0.0.0.0:9090"
allow_root = false

[login]
show_ip = true          # show the server address on the sign-in page
max_failures = 5        # failed attempts per client before a temporary block

[auth]
ssh_keys = true         # allow signing in with a key from ~/.ssh/authorized_keys

[session]
timeout = "12h"         # idle timeout
admin_unlock = "5m"     # root bridge stops after this long without admin calls; "0s" = until sign-out

[tls]
mode = "self-signed"    # self-signed, letsencrypt or custom
redirect = true
# cert = "/path/to/cert.pem"   # used when mode = "custom"
# key  = "/path/to/key.pem"

[plugins]
allow_unsigned = false
dev = false

[updates]
channel = "stable"
auto_check = true
auto_install = false
auto_install_at = "03:30"

[web]
# Extra addresses the console is opened at, e.g. behind a domain name.
allowed_origins = []      # e.g. ["https://admin.example.com"]
# Reverse proxies whose X-Forwarded-Host/-Proto/-For headers are trusted.
trusted_proxies = ["127.0.0.0/8", "::1/128"]
```

If you open the console through a reverse proxy (nginx, Caddy) or a domain name and sign-in fails with "cross-origin request refused", either let the proxy pass the original host (`proxy_set_header Host $host;` or `X-Forwarded-Host`) from a trusted address, or add the address you type in the browser to `web.allowed_origins`. The daemon logs the refused origin with `journalctl -u linuxadmin`. Changes to the file apply without a restart.

The `updates` keys control self-update: the release channel, automatic checks, and an optional nightly install. See [docs/RELEASING.md](docs/RELEASING.md) for how releases are built and signed.

Per-user preferences (theme, language, dashboard layout) live in `~/.config/linuxadmin/`.

## Development

```sh
make build-server        # server/bin/linuxadmind, linuxadmin-bridge, devclient
make test-server         # go vet and go test
make build-web           # web/dist
```

Run the daemon in development mode, which does not need root:

```sh
make dev                 # daemon on 127.0.0.1:9090, proxies the UI to Vite on :5173
make dev-web             # Vite dev server, in a second terminal
make dev-dist            # daemon serving the built web/dist instead of Vite
```

`--dev` runs as your user, listens on plain HTTP on loopback, and lets only your own account sign in. PAM still checks your password, and admin unlock still runs the real `sudo`.

For work that does not need a password, use the no-auth mode:

```sh
make dev-noauth
```

The daemon prints a one-time sign-in URL (`http://127.0.0.1:9090/api/dev/noauth?token=...`). Open it in the browser to get a session cookie. The mode refuses to start without `--dev`, as root, or on a non-loopback address. Admin calls still need a real unlock. `server/tools/devclient` calls the API from the command line.

Web app checks:

```sh
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run i18n:check
npm --prefix web test
```

Contributor notes: [server/README.md](server/README.md), [docs/DESIGN-RULES.md](docs/DESIGN-RULES.md), [web/CONTRIBUTING-SECTIONS.md](web/CONTRIBUTING-SECTIONS.md). The approved screen designs are in `docs/design/`; open `docs/design/index.html`.

## Plugins

A plugin is a folder with `manifest.json` and one ES module. The manifest declares pages, widgets, snippets, the commands the plugin may run, and the files and sockets it may touch. Plugin code runs in a sandboxed frame and talks to the app through a small SDK.

- SDK and examples: [web/PLUGIN-SDK.md](web/PLUGIN-SDK.md)
- Manifest, API and isolation: [docs/api/plugins.md](docs/api/plugins.md)
- Signing: [docs/PLUGIN-SIGNING.md](docs/PLUGIN-SIGNING.md)

The Docker plugin in `plugins/docker` is a working example.

## Project layout

```
server/            Go module: daemon, bridge, modules for each section
  cmd/             linuxadmind, linuxadmin-bridge
  internal/        config, rpc, pam, sys, server, bridge, modules/<section>
  tools/           devclient
web/               React and TypeScript app (Vite)
  src/sections/    one folder per section
plugins/           first-party plugins and the plugin catalog
packaging/         systemd unit, PAM files, install scripts, .deb/.rpm (nfpm) and AUR packaging
docs/              architecture, API notes, design rules, security review
```

## Roadmap

- Test and fix on Debian, Fedora and openSUSE
- Publish the AUR packages, then package repositories (COPR, OBS or an apt repository)
- Desktop app wrapper
- More first-party plugins

## License

MIT. See [LICENSE](LICENSE).
