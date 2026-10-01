# Releasing and self-update

The owner publishes a release by pushing a version tag. GitHub Actions builds it for x86-64 and ARM64, signs it and
publishes it. On every machine running LinuxAdmin, an administrator opens **Settings › About**, clicks
**Update now**, confirms, and the console updates itself; if the new version does not come up, the old one is put
back automatically.

```
git tag v1.2.0 ──▶ .github/workflows/release.yml
                     web (npm ci, build) ──┐
                     build amd64 (ubuntu-24.04)     ──▶ linuxadmin-1.2.0-linux-amd64.tar.gz
                     build arm64 (ubuntu-24.04-arm) ──▶ linuxadmin-1.2.0-linux-arm64.tar.gz
                     release: SHA256SUMS + SHA256SUMS.sig (RELEASE_SIGNING_KEY) ──▶ GitHub release

console ──updates.check──▶ api.github.com/repos/Fonlogen/LinuxAdmin/releases/latest   (cached 1 h, ETag)
        ──updates.apply──▶ download → verify signature + sha256 → extract → run --version → install
                           → systemd-run linuxadmind --apply-update 1.2.0
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

(add `--repo Fonlogen/LinuxAdmin` when running it elsewhere). The workflow writes the secret to a temporary 0600 file,
signs, deletes the file, and then verifies the signature against the key **embedded in the code**: if the secret is
not the key the consoles trust, the release job fails before anything is published.

### Install every machine once with the versioned layout

Self-update needs the layout below. Machines installed before this feature (flat layout: a real
`/usr/bin/linuxadmind`, the web app in `/usr/share/linuxadmin/web`) must be reinstalled once:

```sh
make build VERSION=0.1.0              # as your user, any x.y.z
sudo ./packaging/install-dev.sh       # installs into /usr/lib/linuxadmin/versions/0.1.0
```

or, from a release archive: `tar xzf linuxadmin-1.2.0-linux-amd64.tar.gz && sudo ./linuxadmin-1.2.0-linux-amd64/packaging/install.sh`.
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
   git tag -a v1.2.0 -m "LinuxAdmin 1.2.0" -m "- Self-update from Settings › About"
   git push origin v1.2.0
   gh run watch          # follow the Release workflow
   ```

   * `vX.Y.Z` is a **stable** release, marked "latest": consoles on the `stable` channel (the default) offer it.
   * `vX.Y.Z-rc.1`, `vX.Y.Z-beta.2`… are **pre-releases**: only consoles with `updates.channel = "prerelease"` offer
     them. Ordering follows semver (`1.2.0-rc.1` < `1.2.0`).
   * The version is injected into both binaries (`brand.Version`, `-ldflags -X`) without the `v`; `linuxadmind
     --version` prints it.

4. Check the result: the release has `linuxadmin-1.2.0-linux-amd64.tar.gz`, `linuxadmin-1.2.0-linux-arm64.tar.gz`,
   `SHA256SUMS`, `SHA256SUMS.sig`. To verify by hand:

   ```sh
   gh release download v1.2.0 -D /tmp/r && cd server && go run ./tools/release-sign -verify -dir /tmp/r /tmp/r/SHA256SUMS
   ```

A tag that fails the workflow publishes nothing; fix, delete the tag (`git push --delete origin v1.2.0; git tag -d
v1.2.0`) and tag again. Never re-publish different files under a version that consoles may already have installed:
bump the patch version instead.

`make dist VERSION=1.2.0` builds the same archive locally for this machine's architecture (into `dist/`), unsigned.

## What a console does

### Install layout

```
/usr/lib/linuxadmin/versions/<version>/{bin/linuxadmind, bin/linuxadmin-bridge, web/, plugins/, packaging/, VERSION}
/usr/lib/linuxadmin/current  -> versions/<version>       the running version
/usr/lib/linuxadmin/previous -> versions/<version>       kept for rollback (only these two are kept)
/usr/lib/linuxadmin/linuxadmin-bridge -> current/bin/linuxadmin-bridge   (old unit files)
/usr/bin/linuxadmind -> /usr/lib/linuxadmin/current/bin/linuxadmind
/var/lib/linuxadmin/updates/           last.json (0644), lock, staging/ (0700, downloads)
```

The unit runs `/usr/bin/linuxadmind`; the daemon resolves its real path and takes the bridge, the web app and the
packaged plugins from the same version folder, so a running daemon never mixes versions.

### Update (`updates.apply`, admin)

1. Refuses when this copy is not installed in `/usr/lib/linuxadmin` (a dev build), in `--dev`, while another update
   runs, or while the Software section runs a package transaction (the restart would kill it).
2. Downloads `SHA256SUMS` and `SHA256SUMS.sig` (HTTPS, GitHub hosts only, size limits), verifies the signature with
   the embedded key, then downloads `linuxadmin-<v>-linux-<arch>.tar.gz` (≤ 200 MB) and checks its sha256.
3. Extracts it into `versions/.<v>.partial-*`: only regular files and folders under the one top-level folder; no
   `..`, absolute names, links or devices; at most 20 000 entries, 256 MB per file, 1 GB in total. Checks
   `VERSION` and runs `bin/linuxadmind --version` and `bin/linuxadmin-bridge --version` (they must print the
   version: proves they run on this machine).
4. Renames it to `versions/<v>` and starts `systemd-run --unit=linuxadmin-update-<v>-<time> --collect
   /usr/lib/linuxadmin/versions/<v>/bin/linuxadmind --apply-update <v> --kind update` — a transient unit outside
   `linuxadmin.service`, so it survives the restart.
5. The helper points `current` at `versions/<v>` (temporary symlink renamed over the old one: atomic), runs
   `systemctl restart linuxadmin.service` and polls `https://127.0.0.1:<port>/api/health` until it answers
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
  sudo ln -sfn versions/1.1.0 /usr/lib/linuxadmin/current.tmp && sudo mv -T /usr/lib/linuxadmin/current.tmp /usr/lib/linuxadmin/current
  sudo systemctl restart linuxadmin
  ```

Logs: `journalctl -u linuxadmin` (daemon, automatic installs) and `journalctl -u 'linuxadmin-update-*'` (helper).
The last result is in `/var/lib/linuxadmin/updates/last.json` and in Settings › About.

### Automatic checks and installs

`[updates]` in `/etc/linuxadmin/linuxadmin.conf` (`docs/api/config.md`), editable in Settings › About:

```toml
[updates]
channel = "stable"         # stable | prerelease
auto_check = true          # the daemon checks every 6 h; admins get a bell notification
auto_install = false       # install by itself every day at auto_install_at (local time)
auto_install_at = "03:30"
```

The automatic install runs in the daemon (root) with the same code as the button, logs to the journal, writes
`last.json`, and skips the day when a package transaction is running.

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
machine reinstalled by hand from the archive (`packaging/install.sh`).

## History

* 2026-10-01: release key generated (`eYuaHtzK…`); self-update and the release workflow added.
