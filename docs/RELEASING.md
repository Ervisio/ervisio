# Releasing and self-update

The owner publishes a release by pushing a version tag. GitHub Actions builds it for x86-64 and ARM64 (archives,
`.deb` and `.rpm` packages), signs it and publishes it. New machines install it with the one-line `install.sh`. On
every machine installed that way, an administrator opens **Settings › About**, clicks **Update now**, confirms, and
the console updates itself; if the new version does not come up, the old one is put back automatically. Machines
installed from a package are updated by their package manager instead (`docs/PACKAGING.md`).

```
git tag v1.2.0 ──▶ .github/workflows/release.yml
                     web (npm ci, build) ──┐
                     build amd64 (ubuntu-24.04)     ──▶ ervisio-1.2.0-linux-amd64.tar.gz
                                                        + ervisio_1.2.0_amd64.deb, ervisio-1.2.0-1.x86_64.rpm (nfpm)
                     build arm64 (ubuntu-24.04-arm) ──▶ ervisio-1.2.0-linux-arm64.tar.gz
                                                        + ervisio_1.2.0_arm64.deb, ervisio-1.2.0-1.aarch64.rpm
                     + linuxadmin-1.2.0-linux-{amd64,arm64}.tar.gz (compatibility archives, see "Rename transition")
                     release: SHA256SUMS (all eight files) + SHA256SUMS.sig (RELEASE_SIGNING_KEY) ──▶ GitHub release
                     aur (only with the AUR_SSH_KEY secret): PKGBUILDs ──▶ aur.archlinux.org

install.sh ──▶ api.github.com/…/releases/latest → download → openssl: verify signature + sha256 → versioned layout

console ──updates.check──▶ api.github.com/repos/ervisio/ervisio/releases/latest   (cached 1 h, ETag)
        ──updates.apply──▶ download → verify signature + sha256 → extract → run --version → install
                           → systemd-run ervisiod --apply-update 1.2.0
                              → switch `current` → restart → /api/health answers 1.2.0 within 30 s?
                                 yes: keep, previous = old version     no: switch back, restart old
```

## One-time setup

### The release key

Releases are signed with an ed25519 **release key**, separate from the plugin team key (`docs/PLUGIN-SIGNING.md`).

| Half | Where | In git |
|---|---|---|
| Public | `ReleasePublicKey` in `server/internal/update/sign.go`: `eYuaHtzKiE4ofn2sX/p7tSNX/p5+sGV9EJT0aKSmNU8=` | yes |
| Private | `~/.config/linuxadmin-signing/release.key` on the maintainer's machine (folder 0700, file 0600) and the `RELEASE_SIGNING_KEY` secret of the GitHub repository | **never** |

The key file is one line, the base64 of the 64-byte ed25519 private key (the same format as `plugin-sign`; the tools
share `server/internal/signkey`). Keep an encrypted offline backup (`age -p release.key > release.key.age`). Without
the private key no installed console will accept a new release (see "Rotating the key").

### The GitHub secret

From the repository folder, on the maintainer's machine:

```sh
gh secret set RELEASE_SIGNING_KEY < ~/.config/linuxadmin-signing/release.key
```

(add `--repo ervisio/ervisio` when running it elsewhere). The workflow writes the secret to a temporary 0600 file,
signs, deletes the file, and then verifies the signature against the key **embedded in the code**: if the secret is
not the key the consoles trust, the release job fails before anything is published.

### Install every machine once with the versioned layout

Self-update needs the layout below. Machines installed before this feature (flat layout: a real
`/usr/bin/ervisiod`, the web app in `/usr/share/ervisio/web`) must be reinstalled once:

```sh
curl -fsSL https://raw.githubusercontent.com/ervisio/ervisio/main/install.sh | sudo sh
```

or from a local build (`make build VERSION=0.1.0` as your user, then `sudo ./packaging/install-dev.sh`), or from a
release archive: `tar xzf ervisio-1.2.0-linux-amd64.tar.gz && sudo ./ervisio-1.2.0-linux-amd64/packaging/install.sh`.
(A flat install running a build that already contains the updater is migrated by its first update: the flat files
are copied into `versions/<its version>` before the new one is installed.)

## Cutting a release

1. Make sure `main` is green (CI: `go vet`, `go test -race`, web typecheck/lint/i18n/test/build).
2. Optional but recommended: add a section to `CHANGELOG.md`:

   ```markdown
   ## [1.2.0] - 2026-10-15
   ### Added
   - Self-update from Settings › About
   ```

3. Tag and push. An annotated tag's message is used as release notes when `CHANGELOG.md` has no section for the
   version:

   ```sh
   git tag -a v1.2.0 -m "Ervisio 1.2.0" -m "- Self-update from Settings › About"
   git push origin v1.2.0
   gh run watch          # follow the Release workflow
   ```

   * `vX.Y.Z` is a **stable** release, marked "latest": consoles on the `stable` channel (the default) offer it.
   * `vX.Y.Z-rc.1`, `vX.Y.Z-beta.2`… are **pre-releases**: only consoles with `updates.channel = "prerelease"` offer
     them. Ordering follows semver (`1.2.0-rc.1` < `1.2.0`).
   * The version is injected into both binaries (`brand.Version`, `-ldflags -X`) without the `v`; `ervisiod
     --version` prints it.

4. Check the result: the release has `ervisio-1.2.0-linux-{amd64,arm64}.tar.gz`,
   `ervisio_1.2.0_{amd64,arm64}.deb`, `ervisio-1.2.0-1.{x86_64,aarch64}.rpm`, the compatibility archives
   `linuxadmin-1.2.0-linux-{amd64,arm64}.tar.gz`, `SHA256SUMS` (listing all eight) and `SHA256SUMS.sig`. To verify by hand:

   ```sh
   gh release download v1.2.0 -D /tmp/r && cd server && go run ./tools/release-sign -verify -dir /tmp/r /tmp/r/SHA256SUMS
   ```

   and try the installer against it: `sh install.sh --dry-run --version 1.2.0` (any machine, no root needed).

5. Stable releases only: update the AUR packages. With the `AUR_SSH_KEY` secret the workflow pushes them; either way
   run `packaging/arch/update-pkgbuild.sh 1.2.0` on Arch and commit the result here (`docs/PACKAGING.md`).

A tag that fails the workflow publishes nothing; fix, delete the tag (`git push --delete origin v1.2.0; git tag -d
v1.2.0`) and tag again. Never re-publish different files under a version that consoles may already have installed:
bump the patch version instead.

`make dist VERSION=1.2.0` builds the same archives locally for this machine's architecture (into `dist/`), unsigned.

## What a console does

### Install layout

```
/usr/lib/ervisio/versions/<version>/{bin/ervisiod, bin/ervisio-bridge, web/, plugins/, packaging/, VERSION}
/usr/lib/ervisio/current  -> versions/<version>       the running version
/usr/lib/ervisio/previous -> versions/<version>       kept for rollback (only these two are kept)
/usr/lib/ervisio/ervisio-bridge -> current/bin/ervisio-bridge   (old unit files)
/usr/bin/ervisiod -> /usr/lib/ervisio/current/bin/ervisiod
/var/lib/ervisio/updates/           last.json (0644), lock, staging/ (0700, downloads)
```

The unit runs `/usr/bin/ervisiod`; the daemon resolves its real path and takes the bridge, the web app and the
packaged plugins from the same version folder, so a running daemon never mixes versions.

### Update (`updates.apply`, admin)

1. Refuses when this copy is not installed in `/usr/lib/ervisio` (a dev build), in `--dev`, while another update
   runs, or while the Software section runs a package transaction (the restart would kill it).
2. Downloads `SHA256SUMS` and `SHA256SUMS.sig` (HTTPS, GitHub hosts only, size limits), verifies the signature with
   the embedded key, then downloads `ervisio-<v>-linux-<arch>.tar.gz` (≤ 200 MB) and checks its sha256.
3. Extracts it into `versions/.<v>.partial-*`: only regular files and folders under the one top-level folder; no
   `..`, absolute names, links or devices; at most 20 000 entries, 256 MB per file, 1 GB in total. Checks
   `VERSION` and runs `bin/ervisiod --version` and `bin/ervisio-bridge --version` (they must print the
   version: proves they run on this machine).
4. Renames it to `versions/<v>` and starts `systemd-run --unit=ervisio-update-<v>-<time> --collect
   /usr/lib/ervisio/versions/<v>/bin/ervisiod --apply-update <v> --kind update` — a transient unit outside
   `ervisio.service`, so it survives the restart.
5. The helper points `current` at `versions/<v>` (temporary symlink renamed over the old one: atomic), runs
   `systemctl restart ervisio.service` and polls `https://127.0.0.1:<port>/api/health` until it answers
   `{"version":"<v>"}`. Within 30 s: `previous` becomes the old version and older folders are deleted. Otherwise:
   `current` goes back, the service restarts on the old version, the new folder is deleted, and `last.json` says
   `rolled-back`.

The browser shows each step, then polls `/api/health` and reloads the page when the new version answers. Sessions
live in the daemon's memory, so **everyone has to sign in again** after an update or rollback.

### Rollback

* Automatic, as above, when the new version does not answer within 30 seconds.
* Manual: **Settings › About › Go back to X** (`updates.rollback`) runs the same helper (the running version's
  binary) towards `previous`; afterwards `previous` is the version you left, so you can go forward again.
* By hand on the machine, if the web console is unreachable:

  ```sh
  sudo ln -sfn versions/1.1.0 /usr/lib/ervisio/current.tmp && sudo mv -T /usr/lib/ervisio/current.tmp /usr/lib/ervisio/current
  sudo systemctl restart ervisio
  ```

Logs: `journalctl -u ervisio` (daemon, automatic installs) and `journalctl -u 'ervisio-update-*'` (helper).
The last result is in `/var/lib/ervisio/updates/last.json` and in Settings › About.

### Automatic checks and installs

`[updates]` in `/etc/ervisio/ervisio.conf` (`docs/api/config.md`), editable in Settings › About:

```toml
[updates]
channel = "stable"         # stable | prerelease
auto_check = true          # the daemon checks every 6 h; admins get a bell notification
auto_install = false       # install by itself every day at auto_install_at (local time)
auto_install_at = "03:30"
```

The automatic install runs in the daemon (root) with the same code as the button, logs to the journal, writes
`last.json`, and skips the day when a package transaction is running.

## Rename transition

The product was called **LinuxAdmin** up to 0.2.0 and lived in `Fonlogen/LinuxAdmin`. From 0.3.0 it is
**Ervisio**, in `ervisio/ervisio`: new names for the binaries (`ervisiod`, `ervisio-bridge`), the unit
(`ervisio.service`), the PAM service and every folder (`/etc/ervisio`, `/usr/lib/ervisio`, `/var/lib/ervisio`,
`/usr/share/ervisio`, `~/.config/ervisio`). Machines running LinuxAdmin must get there without anyone logging in to
them, and must be able to go back. The code is `server/internal/legacy`; its tests run the whole move in a fake
root (`legacy_test.go`).

### What does not change

* **The signature prefixes.** Releases are signed over `"linuxadmin-release-v1\n" + SHA256SUMS` and plugins over
  `"linuxadmin-plugin-v1\n" + manifest`. LinuxAdmin consoles verify the release that moves them with the first, and
  every plugin and catalog signed so far uses the second, so both stay as historical constants (`sigPrefix` in
  `update/sign.go`, `signaturePrefix` in `plugins/sign.go`, the `printf` in `install.sh`). Change them only together
  with a key rotation. The same keys sign (`~/.config/linuxadmin-signing/` on the maintainer's machine is just a
  folder name).
* **The SSH key sign-in prefix** did change (`ervisio-ssh-auth-v1`): server and browser are released together. A
  tab still running LinuxAdmin's page reloads after the update anyway.
* **The plugin frame protocol** (`la: 'plugin'` in postMessage) and the Docker plugin's `la.autoupdate` labels stay:
  installed plugins and existing containers use them.
* **The port, address, certificate and configuration**: everything is copied.

### How LinuxAdmin's updater reaches Ervisio

A LinuxAdmin console (0.1.x or 0.2.0) asks `api.github.com/repos/Fonlogen/LinuxAdmin/releases/latest`. After the
repository moves to `ervisio/ervisio`, GitHub answers that with `301` to `/repositories/<id>/releases/latest`; Go's
HTTP client follows it for a GET and keeps the headers (`TestCheckerFollowsMovedRepository`), and the asset URLs of
the answer are `github.com/ervisio/ervisio/releases/download/…`, which the downloader accepts. The updater then
looks for `linuxadmin-<v>-linux-<arch>.tar.gz` with a `linuxadmin-<v>-linux-<arch>/` folder holding
`bin/linuxadmind`, `bin/linuxadmin-bridge`, `web/index.html` and `VERSION`, checks it against the signed
`SHA256SUMS`, runs both binaries with `--version`, installs the folder as `/usr/lib/linuxadmin/versions/<v>` and
starts **the new version's** binary as its switch helper:

```
systemd-run --unit=linuxadmin-update-<v>-<time> /usr/lib/linuxadmin/versions/<v>/bin/linuxadmind --apply-update <v> --kind update
```

So every release publishes a **compatibility archive**, `linuxadmin-<v>-linux-<arch>.tar.gz`
(`packaging/build-release.sh`): the same files as the Ervisio archive with the binaries named `linuxadmind` and
`linuxadmin-bridge`, plus `packaging/linuxadmin.service` and `packaging/pam.d/linuxadmin.*` for LinuxAdmin's
`install.sh --version <v>`. It is listed in `SHA256SUMS`. Keep publishing it for as long as LinuxAdmin consoles may
still be around: one that was offline for a year updates to whatever is latest then. (The 0.2.0 updater was run
against a real compatibility archive, with a test key, from the `v0.2.0` sources: it downloads, verifies,
extracts, probes and launches it unchanged.)

### The transition

The binary started as `--apply-update` from `/usr/lib/linuxadmin/versions/<v>/bin/` sees that it runs from
LinuxAdmin's layout and performs the transition (`legacy.Transition`) instead of a plain switch. It runs in the
transient unit, outside both services:

1. Installs `/usr/lib/linuxadmin/versions/<v>` as `/usr/lib/ervisio/versions/<v>` (binaries renamed),
   `current -> versions/<v>`, `/usr/bin/ervisiod`.
2. Copies the data, leaving an `.imported-from-linuxadmin` marker in the copied folders:
   `/etc/linuxadmin` → `/etc/ervisio` (`linuxadmin.conf` → `ervisio.conf`, paths inside `/etc/linuxadmin/` and
   comments rewritten, `tls/` as it is), `/var/lib/linuxadmin` → `/var/lib/ervisio` (without the download staging
   folder), `/etc/pam.d/linuxadmin` → `/etc/pam.d/ervisio` (comments renamed when LinuxAdmin wrote it, as it is
   otherwise), `linuxadmin.service.d/*.conf` → `ervisio.service.d/`. A destination that already holds Ervisio files
   is never overwritten; empty folders (what a package creates) do not count.
3. Writes `/etc/systemd/system/ervisio.service` from the release (with install.sh's header), `daemon-reload`.
4. Stops `linuxadmin.service`, starts `ervisio.service` (it also has `Conflicts=linuxadmin.service`: one port, one
   service) and waits up to 30 s for `/api/health` to answer `<v>`.
5. **Healthy:** enables `ervisio.service` and disables `linuxadmin.service` (only if LinuxAdmin was enabled), removes
   the markers, writes `/etc/linuxadmin/MOVED-TO-ERVISIO.txt`, moves the scheduled update timer
   (`linuxadmin-update.*` → `ervisio-update.*`), writes `last.json` (`ok`) in both update folders.
   **Not healthy:** stops `ervisio.service`, starts `linuxadmin.service` again, removes everything steps 1-3
   created (the marked folders, the unit, `/usr/lib/ervisio`, `/usr/bin/ervisiod`), drops
   `/usr/lib/linuxadmin/versions/<v>` as LinuxAdmin's own rollback would, and writes `rolled-back` into LinuxAdmin's
   `last.json`, which its Settings › About shows.

LinuxAdmin's page in the browser polls `/api/health` until it answers `<v>` (the same port), then reloads and gets
Ervisio's page. Sessions live in memory, so everyone signs in again, as after any update. Each user's bridge copies
`~/.config/linuxadmin` to `~/.config/ervisio` at their next sign-in (preferences, the Docker plugin's settings), and
the page moves the browser's `la.*` localStorage keys to `ervisio.*` once.

Why this design: the helper is the only code of the new version that LinuxAdmin's updater runs **outside** its
service, as root, before anything is switched, and it is the code that decides what "healthy" means and how to roll
back. Keeping LinuxAdmin's files untouched (copy, never move; `current` of the LinuxAdmin layout is not changed)
makes the rollback a plain `systemctl start linuxadmin`, and makes every step safe to repeat:

* Power cut or crash in the middle: `linuxadmin.service` is still enabled (it is disabled only after Ervisio
  answered), so the machine boots LinuxAdmin; the marked copies are replaced by the next attempt, and a daemon that
  starts meanwhile leaves a marked import alone.
* After a failure LinuxAdmin's updater offers the release again; the next attempt starts from scratch.

### Other ways in

* **A daemon started from LinuxAdmin's layout** by `linuxadmin.service` (LinuxAdmin's `install.sh --version <v>` with
  the compatibility archive, or a manual switch): it serves as usual, imports the data on start, and after 3 seconds
  starts the same transition in a transient unit (`ervisiod --transition <v>`). A failed transition is recorded in
  `/var/lib/linuxadmin/updates/ervisio-transition-failed.json` and not retried on every start for that version; run
  `ervisiod --transition <v>` as root to retry.
* **Ervisio's `install.sh`** on a machine with LinuxAdmin: reads LinuxAdmin's configuration for its questions,
  installs Ervisio, runs `ervisiod --migrate-legacy` (the copy above, final at once, plus the timer), disables
  `linuxadmin.service`, starts `ervisio.service`, and once it runs removes LinuxAdmin's programs, unit and links with
  `ervisiod --remove-legacy` (data kept). A packaged LinuxAdmin is refused with a pointer to the `ervisio` package.
* **Packages:** the `ervisio` `.deb` has `Replaces/Conflicts/Provides: linuxadmin` (dpkg removes `linuxadmin`,
  keeping its configuration), the `.rpm` has `Obsoletes: linuxadmin < <v>` and `Provides: linuxadmin = <v>`; both
  post-install scripts run `ervisiod --migrate-legacy` before anything else, start the service (deferred until the
  package manager exits when it runs inside the console's terminal) and remove a LinuxAdmin made by `install.sh`.
  The AUR packages `ervisio-bin` and `ervisio` conflict with and replace `linuxadmin-bin` and `linuxadmin`; their
  `post_install` migrates.
* **Anything else:** `ervisiod` started as root with the default configuration path copies LinuxAdmin's data when
  `/etc/ervisio` holds no files yet (`legacy.ImportOnStart`).

### Going back, and cleaning up

```sh
sudo systemctl disable --now ervisio && sudo systemctl enable --now linuxadmin   # LinuxAdmin, as it was before the move
sudo ervisiod --remove-legacy            # LinuxAdmin's programs, unit, links, PAM file (when it wrote it), cache
sudo ervisiod --remove-legacy --purge    # also /etc/linuxadmin and /var/lib/linuxadmin
```

Changes made in Ervisio are not copied back. `--remove-legacy` refuses while `linuxadmin.service` runs, while a
transition is unfinished, and for a packaged LinuxAdmin (its package manager removes it).

### Checklist for the first Ervisio release

1. Transfer `Fonlogen/LinuxAdmin` to the `ervisio` organization and rename it `ervisio` (Settings › Transfer). GitHub
   keeps redirecting the old web, git, raw and API URLs as long as no new repository takes the old name: **never
   create a new `Fonlogen/LinuxAdmin`**. `raw.githubusercontent.com/Fonlogen/LinuxAdmin/main/install.sh` keeps
   working through the redirect and serves the new `install.sh`.
2. Set `RELEASE_SIGNING_KEY` (and `AUR_SSH_KEY`) in the new repository if they did not move with it
   (`gh secret list --repo ervisio/ervisio`).
3. Sign `plugins/docker` again (the manifest changed) and commit `manifest.sig`.
4. Tag `v0.3.0`. Check that the release has the eight files, then on a LinuxAdmin 0.2.0 machine: Settings › About
   offers 0.3.0, the update ends on Ervisio, `/etc/linuxadmin/MOVED-TO-ERVISIO.txt` exists.
5. AUR: create `ervisio-bin` and `ervisio`, run `packaging/arch/update-pkgbuild.sh 0.3.0`, publish; mark
   `linuxadmin-bin` and `linuxadmin` for deletion or merge into the new ones (an AUR request), once published.

## Rotating the key

Installed consoles trust only the keys compiled into them (`TrustedKeys` in `server/internal/update/sign.go`). A
new key therefore has to reach them inside a release signed with the old key.

**Planned rotation** (the old key is safe):

1. `go run ./tools/release-sign -genkey ~/.config/linuxadmin-signing/release-<year>.key` (from `server/`; prints the
   public key; never overwrites a file).
2. Add the new public key to `TrustedKeys` next to `ReleasePublicKey` and cut a release **signed with the old key**
   (secret unchanged). Wait until machines have updated to it.
3. Make the new key `ReleasePublicKey`, keep the old one in `TrustedKeys` for one more release, and replace the
   secret: `gh secret set RELEASE_SIGNING_KEY < ~/.config/linuxadmin-signing/release-<year>.key`.
4. One release later, remove the old key from `TrustedKeys`; destroy it and its backups.

**After a leak**: anyone with the old key can sign "updates" that consoles accept. Immediately release a version
whose `TrustedKeys` holds only the new key, signed with the old key (the only key the consoles trust), ask admins
to update (and to turn `auto_install` on or update by hand), then switch the secret to the new key. Mention it in the
release notes.

**Key lost**: consoles cannot verify anything signed with a new key. Generate a new key, release, and have every
machine reinstalled by hand (`install.sh`, or from the archive with `packaging/install.sh`).

`install.sh` trusts one key, `RELEASE_KEY_PEM` (the same key as `ReleasePublicKey`, as an SPKI PEM;
`TestInstallScriptReleaseKey` fails when they differ). Whenever `ReleasePublicKey` changes, convert it with
`python3 -c "import base64,sys; print(base64.b64encode(bytes.fromhex('302a300506032b6570032100')+base64.b64decode(sys.argv[1])).decode())" <key>`
and update the PEM in the same commit. Since the one-line install always fetches `install.sh` from `main`, switch
the secret to the new key only together with that commit.

## History

* 2026-10-01: release key generated (`eYuaHtzK…`); self-update and the release workflow added.
* 2026-10-01: `install.sh`, `.deb`/`.rpm` packages (nfpm), AUR PKGBUILDs, managed-install marker. First release with
  packages: 0.1.1.
* 2026-10-02: LinuxAdmin renamed Ervisio (0.3.0); compatibility archives and the rename transition (see above).
