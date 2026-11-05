# Plugin pages in ErvisiOS

Every enabled, authorized page contributed by a plugin becomes an application automatically. Its public route remains `/p/<plugin>/<page>`. Users can pin it on their desktop or taskbar and organize shortcuts in virtual desktop folders; these folders do not create server filesystem paths.

Existing plugins need no code change: their normal page opens inside an ErvisiOS window and retains the same sandbox, broker, manifest capabilities and server-side checks.

For a native layout, add an optional `ervisios` object to the public page contribution:

```json
{
  "minCore": "0.7.0",
  "contributes": {
    "pages": [{"id":"containers","title":"Containers","ervisios":{"view":"containers-desktop"}}]
  }
}
```

Register both `containers` and `containers-desktop` with `sdk.registerPage`. The desktop chooses the alternate view while classic navigation keeps the public view. The alternate view must be registered by the same plugin module. A native view never changes its permissions.

Contract v4 adds `sdk.shell.mode`, `sdk.view.shell` and the `[data-shell="ervisios"]` CSS attribute. The AI plugin also uses `sdk.shell.command(text)` for deterministic local UI requests. The host verifies the sending frame and manifest identity, permits this operation only for plugin `ai`, caps its text, and resolves strict whole-message UI intents. Unknown messages go to the configured provider; the operation cannot issue RPCs or system commands.

See the SDK repository's [ErvisiOS tutorial](https://github.com/Ervisio/plugin-sdk/blob/main/docs/ervisios.md) for a complete example and compatibility notes. Native manifest metadata requires **Ervisio 0.7.0 or later**: set `minCore` to at least `0.7.0`. Older cores reject unknown manifest fields. Plugins without the new metadata remain compatible and open their existing page in an ErvisiOS window.
