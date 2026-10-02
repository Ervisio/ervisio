# Packaging and distribution

How Ervisio reaches machines, what each way installs, and how to publish it in more places. Building and
signing a release is in [RELEASING.md](RELEASING.md).

## Ways to install

| Way | Who builds it | Layout | Updated by |
|---|---|---|---|
| `install.sh` (one-line install) | release workflow (archive) | versioned | Ervisio itself (Settings > About) |
| `.deb` from the release page | release workflow (nfpm) | flat, managed (`apt`) | the user, with apt/dpkg |
| `.rpm` from the release page | release workflow (nfpm) | flat, managed (`dnf` or `zypper`) | the user, with dnf/zypper |
| AUR `ervisio-bin` | `packaging/arch/ervisio-bin/PKGBUILD` (release archive) | flat, managed (`pacman`) | pacman / AUR helper |
| AUR `ervisio` | `packaging/arch/ervisio/PKGBUILD` (tagged source) | flat, managed (`pacman`) | pacman / AUR helper |
| From source | `make build` + `packaging/install-dev.sh` | versioned | Ervisio itself |

The `.deb` and `.rpm` files on the release page are not in any repository yet, so `apt upgrade` and `dnf upgrade`
do not see new versions: users download the new package and install it over the old one. The console still checks
GitHub and tells administrators when a new version is out. A repository (below) fixes that.

**The first release whose binaries work in the package layout is 0.1.1.** The 0.1.0 daemon looks for the bridge
next to itself and does not know the managed marker, so a 0.1.0 package would not start sessions and would offer a
self-update that replaces files owned by the package manager. Publish packages from 0.1.1 on.

### The two layouts

Versioned (install.sh, self-update, `docs/RELEASING.md`):

```
/usr/lib/ervisio/versions/<v>/{bin,web,plugins,packaging,VERSION}
/usr/lib/ervisio/current -> versions/<v>        previous -> versions/<v before>
/usr/bin/ervisiod -> /usr/lib/ervisio/current/bin/ervisiod
/etc/systemd/system/ervisio.service             /etc/pam.d/ervisio
```

Flat (packages):

```
/usr/bin/ervisiod                         /usr/lib/ervisio/ervisio-bridge
/usr/share/ervisio/{web,plugins}          /usr/lib/ervisio/package-service   (plugins/ is empty)
/usr/lib/ervisio/managed                  /usr/lib/systemd/system/ervisio.service
/etc/pam.d/ervisio                        /etc/ervisio/, /var/lib/ervisio/ (state)
```

The daemon finds its files either way: in a version folder it uses the bridge, web app and plugins of that folder;
otherwise the bridge next to itself or `/usr/lib/ervisio/ervisio-bridge`, the web app in
`/usr/share/ervisio/web` and plugins in `/usr/share/ervisio/plugins`.

### Plugins (the Docker plugin moved to the marketplace)

Releases and packages contain no plugins since 0.4.0: the packaged plugin folder is empty, and plugins are installed
from the signed marketplace catalog into `/var/lib/ervisio/plugins/` (`docs/api/plugins.md`, "Marketplace").
Up to 0.3.0 the Docker plugin was part of every release; updating removes that copy (a new version folder, or the
package manager removing the old files). So that a machine that used it keeps it:

- **Every layout** (install.sh, self-update, `.deb`/`.rpm`, AUR): on its first start the new daemon installs the
  marketplace version into `/var/lib/ervisio/plugins/docker` when the host has a Docker socket and the plugin was not
  switched off (`plugins-state.json`). It uses only the signed catalog, the catalog's checksum and permissions, and a
  package signed with the team key, exactly as an install from Browse. While it cannot (offline, catalog not reachable)
  it retries every hour and Plugins › Installed shows a "Docker moved to the marketplace" card with an Install button.
  The outcome is kept in `/var/lib/ervisio/plugins-moved.json` (`installed`, `skipped`, `not-needed`, `pending`), so it
  happens once: an admin who uninstalls the plugin afterwards does not get it back.
- **install.sh**: when Docker is found and the plugin is not there yet, it asks whether to install it (default yes;
  `--yes` answers yes), and runs `ervisiod --install-plugin docker` with the new binary before the service starts.
  `--with-docker-plugin` installs it without asking (Docker or not); `--no-plugins` runs
  `ervisiod --skip-moved-plugins` (records `skipped`), so neither the installer nor the daemon adds it.
- **Packages** have no questions: the daemon's first start does it as described. To opt out before the first start,
  run `ervisiod --skip-moved-plugins` as root, or switch the plugin off / uninstall it afterwards.

### Managed installs

A package writes `/usr/lib/ervisio/managed` containing the name of its package manager (`apt`, `dnf`, `zypper`,
`pacman`; the `.rpm` picks `dnf` or `zypper` in its post-install script). When the file exists:

- `updates.status` and `updates.check` report `managedBy`, `canUpdate` is false, `updates.apply` and
  `updates.rollback` refuse (`unavailable`), and the daemon never installs anything by itself (it still checks for
  new releases and logs them).
- Settings > About shows "Updates are installed by apt." instead of the Update button, and the automatic-install row
  is disabled.
- `install.sh` refuses to install or uninstall: the package manager owns the files.

`install.sh` installs never write the marker, so they update themselves.

### Restarts from package scripts

Upgrading the package restarts `ervisio.service` (deb, rpm and Arch alike), removing it stops the service. The
package manager may have been started from Ervisio itself, from its terminal or the Software section; it then
runs as a descendant of `ervisiod`, and a restart would kill it halfway. `/usr/lib/ervisio/package-service`
(from `packaging/package-service.sh`) checks the process tree: in that case it hands the restart to a transient
systemd unit that waits until the package manager has exited. Everyone signs in again afterwards.

### What install.sh asks and writes

`install.sh` reads the questions' answers from `/dev/tty`, so they work under `curl | sh` (stdin is the script).
`--yes`, or no terminal, means defaults and no questions; nothing in the script waits for input then. The README
install section lists the options. In short:

- **First install** (no `/etc/ervisio/ervisio.conf`): asks for the port (default 9090, checked with
  `ss`, `netstat` or `/proc/net/tcp`; a systemd socket such as `cockpit.socket` is named), the proxy setup (Caddy or
  another reverse proxy), who can reach the console (`listen`), TLS (`tls.mode`, `tls.cert`, `tls.key`),
  `allow_root`, who may sign in (`auth.allow_users`, `auth.allow_groups`, `auth.admins_only`: every local account,
  only administrators, or named users and groups; options `--allow-users`, `--allow-groups`, `--admins-only`),
  `session.admin_unlock` (5m, 15m, 1h or `0s` = until sign-out), and whether to enable and start the
  service. It prints a summary, asks for confirmation, and writes a fully commented config file.
- **Installed already**: the config file is kept. `--reconfigure` or any configuration option sets only the keys the
  questions cover (an awk edit that keeps everything else, comments included) after copying the file to
  `ervisio.conf.ervisio-backup-<date>`.
- Who may sign in: with nothing set, every account with a login shell in `/etc/shells` can (`nologin`, `false`,
  `git-shell`, `rbash` are refused). The summary warns when the policy would lock out the person running the
  installer (`SUDO_USER`) or leaves no administrator.
- `tls.mode` is `self-signed`, `letsencrypt` (not implemented), `custom` or `http`. With a proxy on this machine
  (native Caddy, Caddy in Docker with `network_mode: host`, or "another reverse proxy on this machine") the installer
  sets `tls.mode = "http"` and `listen = "127.0.0.1:PORT"`: the proxy talks plain HTTP to loopback and keeps the
  session cookie `Secure` through `X-Forwarded-Proto`. Caddy in a bridged Docker container cannot reach the host's
  loopback, so that case keeps HTTPS and `tls_insecure_skip_verify`. A proxy on another machine also keeps HTTPS.
- **Config check.** After writing or changing the file, and before the service is restarted, the installer runs
  `ervisiod --check-config /etc/ervisio/ervisio.conf`. When it fails, the backup is put back (a first
  install removes the file), the errors are printed and the installer stops without restarting. Packages can run the
  same command in their post-install scripts or `ExecStartPre`.

**Signature check.** In order: `openssl` (3 or newer), an `openssl3` binary, `python3`, `python`, `python2`. Each
candidate must verify the RFC 8032 test 2 vector and refuse a wrong message before it is trusted. OpenSSL 1.0.2 and
1.1.1 (Amazon Linux 2, CentOS 7) have no `pkeyutl -rawin`, so on those systems the embedded pure-Python verifier
(`write_pyverify` in `install.sh`, after the RFC 8032 section 6 reference code, Python 2.7 and 3) does the check;
Amazon Linux 2 always has Python 2 because yum needs it. If nothing works the script stops before downloading, unless
`--insecure-skip-signature` is given.

**Caddy.** Detection: a `caddy` binary or `caddy.service` plus a Caddyfile (the `--config` of the unit, else
`/etc/caddy/Caddyfile`), or a running container whose image name contains `caddy`, found with
`docker -H unix:///var/run/docker.sock` (the default socket; `DOCKER_HOST` and contexts are ignored on purpose). For a
container the Caddyfile path comes from its `--config` argument (default `/etc/caddy/Caddyfile`) and is mapped to the
host path through `docker inspect` mounts. The edit is done by a small awk block parser: it follows top-level blocks
by brace depth (placeholders like `{http.request.host}` and quoted strings are skipped) and gives up, printing the
snippet instead, on anything else. A site block whose address contains `cockpit` gets its single `reverse_proxy`
line (or block) replaced; otherwise a new site block is appended. The file is written in place (`cat > file`), not
renamed, because a single file mounted into a container must keep its inode. The same checks run before and after:
`caddy validate` (or `docker exec ... caddy validate`); a failure after the edit restores the backup.

### Switching between install.sh and a package

- From install.sh to a `.deb` or `.rpm`: install the package. Its post-install script removes the versioned layout
  and the unit file that install.sh wrote, and restarts the service on the packaged files. Configuration and the
  TLS certificate stay.
- From install.sh to the AUR package: pacman refuses to overwrite `/usr/bin/ervisiod`, which it does not own. Run
  `sudo sh install.sh --uninstall` first (it keeps `/etc/ervisio`), then install the package.
- From a package to install.sh: remove the package (`apt remove ervisio`, not purge, keeps the configuration),
  then run install.sh.

### Replacing LinuxAdmin

Ervisio was called LinuxAdmin up to 0.2.0; its packages were `linuxadmin` (`.deb`, `.rpm`) and `linuxadmin-bin` /
`linuxadmin` (AUR). The Ervisio packages take their place (`docs/RELEASING.md`, "Rename transition"):

- `.deb`: `Replaces`, `Conflicts` and `Provides: linuxadmin`. `apt install ./ervisio_<v>_amd64.deb` removes
  `linuxadmin` (not purged: `/etc/linuxadmin` and `/var/lib/linuxadmin` stay) and the post-install script copies them
  with `ervisiod --migrate-legacy` before starting `ervisio.service`. The deb ships its own `/etc/pam.d/ervisio`, so
  local changes to `/etc/pam.d/linuxadmin` have to be carried over by hand.
- `.rpm`: `Obsoletes: linuxadmin < <v>` and `Provides: linuxadmin = <v>` (nfpm writes `replaces` as Obsoletes).
  `%post` runs before the old package is erased, copies the data (the PAM file included, kept as it is when it was
  changed), and starts `ervisio.service`, which stops `linuxadmin.service` (`Conflicts=`); the old package's `%preun`
  then only disables a stopped unit.
- AUR: `ervisio-bin` and `ervisio` conflict with `linuxadmin`, `linuxadmin-bin` and each other, and replace the
  package of the same kind. pacman asks to remove the LinuxAdmin package (its `pre_remove` stops it); `post_install`
  copies the data. Then `systemctl enable --now ervisio`.
- A LinuxAdmin installed with its `install.sh`: the `.deb` and `.rpm` scripts also remove its programs and unit
  (`ervisiod --remove-legacy`) once `ervisio.service` runs; Ervisio's `install.sh` does the same.
  Ervisio's `install.sh` refuses a LinuxAdmin installed from a package: install the `ervisio` package instead.

## Distribution details

PAM files, one per family, in `packaging/pam.d/`:

| Family | `ID` / `ID_LIKE` in `/etc/os-release` | Stacks |
|---|---|---|
| `arch` | arch, manjaro, endeavouros… | `system-login` |
| `debian` | debian, ubuntu, linuxmint, pop… | `@include common-auth`, `common-account`, `common-session` |
| `fedora` | fedora, rhel, centos, rocky, almalinux… | `password-auth`, `postlogin` |
| `suse` | opensuse-*, sles | `common-auth`, `common-account`, `common-session` |

`install.sh` and `install-dev.sh` pick the file for the running system (falling back to the PAM files present on
disk when the distribution is not listed); the `.deb` ships the `debian` file as a conffile; the `.rpm` ships the
`fedora` and `suse` files in `/usr/share/ervisio/pam.d/` and copies the right one to `/etc/pam.d/ervisio` when
that file does not exist yet; the PKGBUILDs ship the `arch` file. `install.sh` carries copies of the four files (for
release archives that do not include them), and `TestInstallScriptPAMCopies` keeps them equal.

Administrator rights go through sudo with the user's own password:

| Family | Admin group | Default sudoers |
|---|---|---|
| Arch | `wheel` | rule commented out: enable `%wheel ALL=(ALL:ALL) ALL` with `visudo` |
| Debian, Ubuntu | `sudo` | `%sudo ALL=(ALL:ALL) ALL` active |
| Fedora, RHEL | `wheel` | `%wheel ALL=(ALL) ALL` active |
| openSUSE | `wheel` (may need `groupadd wheel`) | `Defaults targetpw` asks for root's password: install `sudo-policy-wheel-auth-self` |

The release binaries need glibc 2.17 or newer, so they also run on older systems such as Amazon Linux 2, RHEL 8,
Debian 10/11 and Ubuntu 20.04. They are built with cgo against libpam in a manylinux2014 container
(`packaging/build-compat.sh`, used by the release workflow), which refuses binaries that reference newer glibc symbols.
A plain `make build-server` links against the build machine's glibc instead.

Tested in containers (systemd as PID 1) on Arch Linux, Debian 12, Ubuntu 24.04, Fedora 44 and openSUSE Tumbleweed:
`install.sh` from GitHub (signature check with the distribution's openssl; on Amazon Linux 2 with the Python verifier,
dry run only), upgrade, repair, uninstall, PAM sign-in,
admin unlock through sudo, the `.deb`/`.rpm`/pacman package install, upgrade, deferred restart and removal.
ARM64 was not tested.

## Building the packages

The release workflow (`.github/workflows/release.yml`) does it for every tag, from the release archive itself:

```sh
packaging/build-release.sh 1.2.0 amd64 dist                      # the archive
tar xzf dist/ervisio-1.2.0-linux-amd64.tar.gz -C /tmp/rel
NFPM=/path/to/nfpm packaging/build-packages.sh /tmp/rel/ervisio-1.2.0-linux-amd64 amd64 dist
# -> dist/ervisio_1.2.0_amd64.deb, dist/ervisio-1.2.0-1.x86_64.rpm
```

nfpm is pinned in the workflow (`NFPM_VERSION`, with the sha256 of each binary from nfpm's `checksums.txt`). To bump
it, download the new `checksums.txt` from https://github.com/goreleaser/nfpm/releases and update the three values in
`release.yml` and the two in `ci.yml`. The nfpm configuration and the deb/rpm scripts are in `packaging/pkg/`.

Package file names never contain `~` (nfpm turns `1.2.0-rc.1` into `1.2.0~rc.1` inside the package, the file name
keeps the `-`): every console parses `SHA256SUMS`, and its name check does not accept `~`.

## Publishing on the AUR

Two packages: `ervisio-bin` (release archives, quick to install) and `ervisio` (built from the tag with Go and
npm). They conflict with each other; `ervisio-bin` provides `ervisio`.

The project is MIT licensed; the packages install the license to `/usr/share/licenses/`.

### One-time setup

1. Create an account on https://aur.archlinux.org/register (confirm the e-mail).
2. Make an SSH key only for the AUR and add the public half under "My Account" > "SSH Public Key":

   ```sh
   ssh-keygen -t ed25519 -C "aur@fonlogen" -f ~/.ssh/aur
   cat ~/.ssh/aur.pub
   ```

3. Tell SSH to use it:

   ```
   # ~/.ssh/config
   Host aur.archlinux.org
     IdentityFile ~/.ssh/aur
     User aur
   ```

### First upload (and every release by hand)

```sh
# in this repository, on Arch, as your user:
packaging/arch/update-pkgbuild.sh 0.3.0      # checks the release signature, writes pkgver, checksums, .SRCINFO
git diff -- packaging/arch && git commit -am "AUR: 0.3.0"

# test-build without installing:
cd packaging/arch/ervisio-bin && makepkg -f && cd -

# publish each package
cd ~/aur
git clone ssh://aur@aur.archlinux.org/ervisio-bin.git     # an empty repository the first time: that creates the package
cd ervisio-bin
cp <this repository>/packaging/arch/ervisio-bin/{PKGBUILD,.SRCINFO,ervisio.install,ervisio.pam,package-service.sh} .
git add PKGBUILD .SRCINFO ervisio.install ervisio.pam package-service.sh
git commit -m "Update to 0.3.0"
git push                                                    # the AUR branch is master
```

Repeat with `ervisio` (`ssh://aur@aur.archlinux.org/ervisio.git`). The PKGBUILDs in this repository have
`SKIP` checksums until `update-pkgbuild.sh` has run for the first Ervisio release. If `linuxadmin-bin` and
`linuxadmin` were published, file a merge request on the AUR for each (into `ervisio-bin` and `ervisio`) once the
new packages exist, so their users get them. Always commit `.SRCINFO` together with the
`PKGBUILD`; the AUR rejects pushes where they disagree. Once published, users install with `yay -S ervisio-bin`
(or `paru`), then `sudo systemctl enable --now ervisio`.

### Automatic upload from the release workflow

The `aur` job in `release.yml` does the steps above for stable tags when the repository has the secret
`AUR_SSH_KEY` (the private key made above, without a passphrase). Without the secret the job is skipped.

```sh
gh secret set AUR_SSH_KEY < ~/.ssh/aur
```

The job runs `update-pkgbuild.sh` in an Arch container, pushes both packages and uploads the updated
`packaging/arch` folder as a workflow artifact. It does not commit to this repository: run `update-pkgbuild.sh`
locally and commit, so the PKGBUILDs here stay current. It trusts the AUR host key it scans; to pin it, replace
`ssh-keyscan` with the fingerprints published on https://aur.archlinux.org.

## Fedora COPR

COPR builds RPMs from a source RPM and hosts a dnf repository, so users get updates with `dnf upgrade`:
`sudo dnf copr enable fonlogen/ervisio && sudo dnf install ervisio`.

1. Fedora account at https://accounts.fedoraproject.org, then sign in to https://copr.fedorainfracloud.org.
2. API token from https://copr.fedorainfracloud.org/api/ into `~/.config/copr`; `sudo dnf install copr-cli`.
3. A spec file is still needed (`packaging/rpm/ervisio.spec`, not written yet): build from the tag like the
   source PKGBUILD (Go with cgo + libpam, `npm ci && npm run build`), install the flat layout, write the marker
   `dnf`, use `%systemd_post`/`%systemd_preun`/`%systemd_postun_with_restart` and the Fedora PAM file. COPR chroots
   have network access when "Enable internet access during builds" is set on the project, which `npm ci` and
   `go mod download` need.
4. `copr-cli create ervisio --chroot fedora-rawhide-x86_64 --chroot fedora-43-x86_64 --chroot epel-9-x86_64 --enable-net on`
5. Per release: `rpmbuild -bs ervisio.spec` then `copr-cli build ervisio ervisio-<v>-1.src.rpm`, or connect
   the GitHub repository in the COPR web UI (Packit or the SCM source type) so tags build by themselves.

## Ubuntu PPA, or an apt repository of our own

**PPA (Launchpad)**: Launchpad only builds from a signed Debian source package (`debian/` folder: `control`, `rules`,
`changelog`, `ervisio.postinst`…) uploaded with `dput`, and builds without network access, so the Go modules
(`go mod vendor`) and the npm dependencies (or the built `web/dist`) must be inside the source package. Steps:
Launchpad account, OpenPGP key registered on Launchpad, signed Ubuntu Code of Conduct, create the PPA, then per
release and per Ubuntu series `debuild -S -sa` and `dput ppa:fonlogen/ervisio ervisio_<v>_source.changes`.
More work than the other options because of the offline build.

**Own repository**: publish the `.deb` files the release already builds in a signed apt repository, for example on
GitHub Pages or a small server:

```sh
# once: a signing key (keep the private half offline like the release key)
gpg --quick-gen-key "Ervisio packages <info.fonlogen@gmail.com>" ed25519 sign never
# per release, in the repository folder (reprepro with conf/distributions listing "stable" and the key)
reprepro includedeb stable ervisio_<v>_amd64.deb ervisio_<v>_arm64.deb
```

Users then add `/etc/apt/sources.list.d/ervisio.sources` with `Signed-By:` pointing to the published key and get
updates through `apt upgrade`. The same can be done for RPMs with `createrepo_c` (a signed `repomd.xml`). Hosted
services such as Cloudsmith or packagecloud have free plans for open-source projects and do both.

## openSUSE Build Service (OBS)

https://build.opensuse.org builds and hosts repositories for openSUSE, SLES, Fedora, RHEL clones, Debian and Ubuntu
from one project, so it can replace COPR and the apt repository.

1. openSUSE account, then `osc` (`zypper install osc`, or `pip install osc`).
2. `osc checkout home:Fonlogen && cd home:Fonlogen && osc mkpac ervisio`.
3. Add the spec file (the COPR one, with `%if 0%{?suse_version}` for the openSUSE PAM file and `zypper` marker), a
   `_service` file that fetches the tag (`obs_scm`) and vendors the Go modules (`go_modules` service); OBS builds
   offline, so the web app must be built beforehand and shipped as an extra source (e.g. a `web-dist-<v>.tar.gz`
   release asset).
4. Enable the target repositories in the project's Repositories tab, `osc commit`, and watch the build.
5. Users: `zypper addrepo https://download.opensuse.org/repositories/home:Fonlogen/openSUSE_Tumbleweed/home:Fonlogen.repo`.

## Official distribution inclusion

What every distribution expects before a package enters its official repositories:

- **A license** approved by OSI and acceptable to the distribution (Debian: DFSG-free). This is the first blocker.
- **Builds from source without network access**, with every dependency either packaged in the distribution or
  declared as bundled. For Ervisio that means the Go modules (packaged as `golang-*` in Debian/Fedora, or vendored
  with a bundling declaration) and the npm dependencies of the web app, which is the hard part.
- **No self-update**: the managed marker already turns it off.
- **Distribution conventions**: systemd presets (services are not enabled by default on Fedora and Debian unless
  the distribution allows it), FHS paths, hardening flags, no bundled copies of system libraries.
- **A maintainer** inside the distribution:
  - Arch: AUR first; a Package Maintainer may adopt a popular AUR package into `[extra]`.
  - Debian/Ubuntu: file an ITP (Intent To Package) bug, prepare the package to Debian policy, find a Debian Developer
    to sponsor uploads (mentors.debian.net). Ubuntu then syncs from Debian.
  - Fedora/EPEL: open a package review request in Bugzilla, get a sponsor to join the packager group, follow the Go
    and Node.js packaging guidelines.
  - openSUSE: build in a devel project on OBS, then send a submit request to `openSUSE:Factory`.
- **Track record**: regular releases, a security contact and a way to report vulnerabilities, a changelog.
