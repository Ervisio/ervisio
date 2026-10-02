# Plugin signing key

Ervisio trusts plugins signed with one ed25519 key, the **team key**. (Releases of Ervisio itself are signed
with a different key, the release key: `docs/RELEASING.md`.) Since `plugins.allow_unsigned = false` is
the default, a plugin that is not signed with it does not run (except dev folders in developer mode, marked
"Unsigned, dev"). The signature format is in `docs/api/plugins.md`, "Signing".

## Where the key is

| Half | Where | In git |
|---|---|---|
| Public | `TeamPublicKey` in `server/internal/modules/plugins/sign.go`: `reTBt4sr4E7AYindYYzscF4oUSPzf1hSQ7d8f/R2BfU=` | yes |
| Private | `~/.config/linuxadmin-signing/team.key` on the release maintainer's machine (folder 0700, file 0600, base64 of the 64-byte ed25519 private key, as written by `plugin-sign -genkey`) | **never** |

Rules:

* The private key never goes into the repository, a CI log, an issue or a chat. It is outside the working tree on
  purpose; do not copy it into it, not even temporarily. `git status` must never show a `*.key` file.
* Keep one offline backup (encrypted, for example `age -p team.key > team.key.age` on a USB stick in a safe place).
  Without a backup, losing the laptop means rotating the key and re-signing every first-party plugin.
* Only release maintainers hold a copy. If CI ever signs, give it the key as a masked secret and write it to a temporary
  file with mode 0600 that the job deletes; never pass it on the command line.

## Signing a plugin

```sh
cd server && go build -o /tmp/plugin-sign ./internal/modules/plugins/cmd/plugin-sign && cd ..
/tmp/plugin-sign -key ~/.config/linuxadmin-signing/team.key plugins/docker
/tmp/plugin-sign -verify plugins/docker        # "signature ok" against the embedded key
```

Signing rewrites `manifest.json` (keys sorted, `files` filled with the sha256 of every file) and writes `manifest.sig`.
Sign after the last change to any file of the plugin, and commit `manifest.json` and `manifest.sig` together with the
change. `go test ./internal/modules/plugins` fails (`TestShippedDockerSigned`) when the shipped Docker plugin no longer
matches its signature.

## Rotating the key

Rotate when the private key may have leaked, when a maintainer with a copy leaves, or when it is lost.

1. `plugin-sign -genkey ~/.config/linuxadmin-signing/team-<year>.key` (prints the new public key).
2. Put the new public key in `TeamPublicKey`. For a planned rotation, keep the old one trusted for one release by
   adding it to `TrustedKeys` in `sign.go`; after a leak, remove it at once.
3. Re-sign every first-party plugin (`plugins/*`) and the packages listed in the catalog, publish them, and release.
4. Destroy the old private key and its backups (after a leak), and say in the release notes that plugins signed with
   the old key stop verifying.

## History

* 2026-10-01: first real team key generated (replaced the placeholder whose private key had been thrown away);
  `plugins/docker` 1.4.1 signed with it.
