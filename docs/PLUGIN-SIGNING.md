# Plugin signing key

Ervisio trusts plugins signed with one ed25519 key, the **team key**. The same key signs the marketplace catalog
(`catalog.sig`). (Releases of Ervisio itself are signed with a different key, the release key: `docs/RELEASING.md`.) Since `plugins.allow_unsigned = false` is
the default, a plugin that is not signed with it does not run (except dev folders in developer mode, marked
"Unsigned, dev"). The signature format is in `docs/api/plugins.md`, "Signing".

## Where the key is

| Half | Where | In git |
|---|---|---|
| Public | `TeamPublicKey` in `server/internal/modules/plugins/sign.go`: `reTBt4sr4E7AYindYYzscF4oUSPzf1hSQ7d8f/R2BfU=` | yes |
| Private | `~/.config/linuxadmin-signing/team.key` on the release maintainer's machine (folder 0700, file 0600, base64 of the 64-byte ed25519 private key, as written by `plugin-sign -genkey`) | **never** |
| Private, CI copy | repository secret `PLUGIN_SIGNING_KEY` of [Ervisio/plugins](https://github.com/Ervisio/plugins) (the file's content), used only by its `publish` workflow | **never** |

Rules:

* The private key never goes into the repository, a CI log, an issue or a chat. It is outside the working tree on
  purpose; do not copy it into it, not even temporarily. `git status` must never show a `*.key` file.
* Keep one offline backup (encrypted, for example `age -p team.key > team.key.age` on a USB stick in a safe place).
  Without a backup, losing the laptop means rotating the key and re-signing every first-party plugin.
* Only release maintainers hold a copy, plus the registry's CI secret. The `publish` workflow writes the secret to a
  temporary file with mode 0600 that the job deletes, never passes it on the command line, and checks every signature
  it makes against the key embedded in Ervisio (a wrong secret fails the job). Set or replace the secret with
  `gh secret set PLUGIN_SIGNING_KEY --repo Ervisio/plugins < ~/.config/linuxadmin-signing/team.key` (from the
  maintainer's machine; nothing is printed). Protect `main` of the registry (pull requests and a review required):
  whoever can push there can get a plugin signed.

## Signing a plugin

Normally nobody signs by hand: a plugin is published through the registry (`docs/api/plugins.md`, "Marketplace").
After a maintainer merges the pull request that adds or updates it, the registry's `publish` workflow signs it, attaches
the signed tarball to a release `<id>-<version>` of Ervisio/plugins, regenerates and signs `catalog.json` and publishes
both on GitHub Pages.

By hand (a private build, or a check):

```sh
cd server && go build -o /tmp/plugin-sign ./internal/modules/plugins/cmd/plugin-sign && cd ..
/tmp/plugin-sign -key ~/.config/linuxadmin-signing/team.key path/to/plugin    # the folder holding manifest.json
/tmp/plugin-sign -verify path/to/plugin        # "signature ok" against the embedded key
/tmp/plugin-sign -verify -catalog catalog.json # a downloaded catalog.json with its catalog.sig
```

Signing rewrites `manifest.json` (keys sorted, `files` filled with the sha256 of every file) and writes `manifest.sig`.
Sign after the last change to any file of the plugin.

## Rotating the key

Rotate when the private key may have leaked, when a maintainer with a copy leaves, or when it is lost.

1. `plugin-sign -genkey ~/.config/linuxadmin-signing/team-<year>.key` (prints the new public key).
2. Put the new public key in `TeamPublicKey`. For a planned rotation, keep the old one trusted for one release by
   adding it to `TrustedKeys` in `sign.go`; after a leak, remove it at once.
3. Update `PLUGIN_SIGNING_KEY` and `team.pub` in Ervisio/plugins, then re-run its `publish` workflow so that every
   listed plugin and the catalog are signed again (delete the old `<id>-<version>` releases first, or bump the
   versions), and release Ervisio with the new key.
4. Destroy the old private key and its backups (after a leak), and say in the release notes that plugins signed with
   the old key stop verifying.

## History

* 2026-10-01: first real team key generated (replaced the placeholder whose private key had been thrown away);
  `plugins/docker` 1.4.1 signed with it.
* Ervisio 0.4.0 (unreleased): the Docker plugin left the core; plugins and the marketplace catalog are signed by the
  registry's CI with the team key (secret `PLUGIN_SIGNING_KEY` of Ervisio/plugins).
