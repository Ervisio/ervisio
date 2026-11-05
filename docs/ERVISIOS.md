# ErvisiOS

ErvisiOS is an optional windowed interface for the same Ervisio server console. The classic console stays available. It uses the existing authenticated APIs, file manager, PTYs, plugin sandbox and administrator unlock.

## Choose the interface

ErvisiOS is included in stable Ervisio releases starting with **0.7.0**. Install or upgrade normally, then choose the interface during installation or in Settings. Classic remains the default for unattended fresh installations.

To build from source instead:

```sh
git clone https://github.com/Ervisio/ervisio.git
cd ervisio
npm --prefix web ci
npm --prefix web run build
make build-server
./server/bin/ervisiod --dev --web web/dist
```

Use the existing [server prerequisites](../server/README.md) for your platform. Sign in normally, then select ErvisiOS in Appearance. Plugin development builds can be copied from `dist/<plugin-id>/` into this checkout's ignored `plugins/<plugin-id>/` folder. The daemon's `--dev` mode loads these local unsigned builds; production installations use the signed marketplace.

The Linux installer accepts `--ui ervisios` or `ERVISIO_UI=ervisios`; the Windows installer accepts `-UI ervisios` or the `ERVISIO_UI` environment variable. Use `classic` instead to select the classic console. Interactive Linux installations offer the choice. This sets `ui.default_shell` in the server configuration. Upgrades preserve the existing choice unless explicitly changed.

After sign-in, **Settings → Appearance → Interface** changes your own interface immediately. The per-user `shell.mode` overrides the installation default. The login page uses the installation default or that username's previously selected interface. Password and SSH-key sign-in keep their normal security behavior.

## Desktop and applications

- Every authorized core section and installed plugin page appears in Start automatically. Native plugin layouts are used when declared; other plugins retain their current page inside a window.
- Open several applications, minimize, maximize, resize, or snap a window to either side. Dragging to the top maximizes; dragging a maximized or snapped titlebar restores its previous normal size under the pointer. Start search selects its first result; arrows change selection and Enter opens it. The taskbar restores running windows. Alt+Tab cycles apps, Alt+F4 closes the active window, Ctrl+Alt+D shows the desktop and Ctrl+K opens the existing command palette.
- Use an app's menu to add a desktop shortcut or pin it to the taskbar. Drag shortcuts into virtual folders, including nested folders. The keyboard-accessible Move to folder action offers the same organization.
- Desktop folders only store app shortcuts. Removing a folder promotes its contents; it never deletes server files or uninstalls applications.
- Wallpaper, folders, shortcuts, taskbar pins and window bounds persist through the existing per-user preferences API. They follow the account between browsers. Failed preference loads block editing and offer Retry, preventing defaults from replacing an unreadable saved desktop.
- This PC opens real home/system locations through File Explorer. File Explorer retains tabs, split views, uploads, downloads, permissions, SFTP configuration and transfer progress. Dropping files or folders from the computer onto the desktop asks for a real server destination and uses the same transfer queue; dropping directly into Explorer uses its current folder.

## Terminal

The native Terminal layout uses real terminal sessions with tabs, local/root/SSH profiles, splits, snippets, command palette and existing session controls. Its appearance dialog saves opacity, backdrop blur, font family/size, palette, cursor, line height and scrollback per user. Appearance changes update xterm without restarting the PTY. The classic terminal remains available in the classic interface.

## Connectivity

Quick settings reads the **server's** interfaces, addresses and Wi-Fi/Bluetooth hardware. Missing hardware, unavailable tools, blocked radios and query failures are explicit states. No fictional Wi-Fi network or Bluetooth device is supplied. Ethernet and connection names are shown when reported by the operating system.

Radio changes call the operating system through `system.connectivity.set`, require administrator rights and return fresh state. On Linux this uses NetworkManager and BlueZ where installed; on Windows it uses the OS radio API. OS policy and physically blocked hardware can refuse a change. Network discovery, provisioning a missing driver and pairing devices are outside this interface's current controls.

## AI and native plugins

The assistant uses [plugin-ai](https://github.com/Ervisio/plugin-ai). Its ErvisiOS-aware release handles exact local navigation/theme/window commands immediately without a provider or engine; other requests use its existing provider and tool flow. API keys, provider support, permissions, tool policies and administrator unlock retain their plugin behavior. Conversations are discovered from each user's server transcript files, restored on reopening and checkpointed before requests and after completed exchanges. Closing a browser during a streaming reply can still interrupt unfinished output; previously saved exchanges remain.

Native layouts are supported for AI, Docker, Minecraft and Caddy through their corresponding plugin releases. Existing plugin releases keep working inside desktop windows. Plugin authors should follow [the native page contract](dev/ervisios-plugins.md) and [the SDK tutorial](https://github.com/Ervisio/plugin-sdk/blob/main/docs/ervisios.md). A manifest declaring `contributes.pages[].ervisios` must set **`minCore: "0.7.0"` or later**; older cores reject the new manifest field. Publish and validate each native plugin release against Ervisio 0.7.0 or later before advertising it in the signed marketplace.

## Verification

From `web/`, run `npm ci`, `npm run typecheck`, `npm run lint`, `npm run i18n:check`, `npm test`, `npm run build` and `npm run test:e2e`. Install a browser first with `npx playwright install chromium`, or use `PLAYWRIGHT_CHANNEL=chrome` for an installed Chrome. The browser tests use the existing mock daemon for deterministic desktop interaction, not as proof of system execution.

`scripts/qa-plugin-frames.mjs` and `scripts/verify-plugin-frames.mjs` exercise built native/classic plugin bundles inside the actual sandbox runtime using in-memory fixtures. Build the sibling plugin repositories first. `scripts/verify-ervisios-host.mjs` separately verifies actual upload, download and Explorer against an isolated no-mock loopback development daemon, with authenticated browser storage under `.qa/`; it refuses a non-loopback base URL. Test assets, browser sessions and credentials under `.qa/` are ignored by Git.

The Windows development-host checks verified an actual ConPTY command, appearance changes without a PTY restart, persistence after reload, and upload/download byte equality. Hardware queries were tested read-only; real radio changes and real Linux game/container/web services require the target platform and were not executed on this workstation.
