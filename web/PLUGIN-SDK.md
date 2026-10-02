# Plugin SDK

The plugin SDK (contract version 3) lives in its own repository: **https://github.com/Ervisio/plugin-sdk**.

* `docs/sdk.md` there: the SDK object (`sdk.api.exec`, `http`, `pty`, `files`, `asset`, `registerPage`, ...), styling,
  developing, migrating from v1/v2 and the security notes (what used to be this file).
* `@ervisio/plugin-sdk`: TypeScript types, the `react` shim and the Vite preset that builds one self-contained ES
  module; `template/` is a minimal plugin to start from.
* `docs/publishing.md`: how to publish a plugin to the marketplace (registry https://github.com/Ervisio/plugins).

What stays here, in this repository:

* The runtime side of the contract: the sandboxed frame and its runtime (`web/src/plugins/frame`), the broker
  (`web/src/plugins/broker.ts`) and the message protocol (`web/src/plugins/protocol.ts`). A change to them that plugins
  can see must be reflected in the SDK (and the contract version bumped when it breaks plugins).
* The manifest, the daemon methods and the isolation rules: `docs/api/plugins.md`.
* Signing: `docs/PLUGIN-SIGNING.md`.

The Docker plugin (https://github.com/Ervisio/plugin-docker) is a complete example built with the SDK.
