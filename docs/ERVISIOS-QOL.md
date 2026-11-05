# ErvisiOS quality-of-life guide

These changes make server administration more convenient. The classic console and ErvisiOS use the same authenticated backend, file permissions, terminal sessions and plugin capabilities.

## Open a terminal in a folder

In File Explorer, right-click a directory or the empty space inside the current directory and choose **Open in Terminal**. For a file, the action opens its containing directory. This is available in both interfaces.

Ervisio starts a real terminal session with that directory as its working directory. For example, open a project folder, choose **Open in Terminal**, then run `claude` if Claude Code is installed and configured for that server account. Ervisio does not install or authenticate external command-line tools through this action.

An elevated local file view uses the normal administrator unlock flow. The directory is passed to the terminal backend as session metadata, rather than concatenated into a `cd` command. Remote network folders do not offer this local-terminal action.

In ErvisiOS, Terminal tabs share the window's top bar with the new-tab and window controls. **Commands** opens the command/help interface; terminal appearance settings remain a separate action. Local, administrator and SSH sessions, splits and the existing terminal settings remain available.

## Notepad and Code editor

Both applications are available from the classic console's navigation and ErvisiOS Start:

| Application | Use |
| --- | --- |
| **Notepad** | Plain text, notes and small logs, with document tabs, find, word wrap and adjustable text size. |
| **Code editor** | Configuration and source files, with syntax highlighting, line numbers, indentation and find/replace. It uses CodeMirror and loads language support when needed. |

Double-clicking a small text file opens the appropriate editor. The file's context menu also offers **Open in Notepad** and **Open in Code editor**. Text and log files default to Notepad; other recognized text/code files default to Code editor. Syntax can be selected manually when a filename does not identify a language.

- **Ctrl+O** opens a server file; **Ctrl+S** saves; **Ctrl+Shift+S** opens Save as. **Ctrl+F** opens search.
- Changes are saved to the server file, including files opened from a saved network connection. To open a network file, browse to it in File Explorer first.
- Document tabs keep separate text and saved states. Closing an edited tab or ErvisiOS window asks whether to keep editing or discard changes; reloading or leaving the browser page invokes the browser's unsaved-changes protection.
- Saving an existing file checks its modification time. If it changed on the server, Ervisio reports the conflict; copy the edits or save another file before reloading.
- New documents use UTF-8. Opened files retain the detected encoding and LF/CRLF line ending. Large previews are read-only; binary files are not text-editable. File Explorer's direct editing threshold is 2 MiB.

Code editor is a small file editor, with no language server, project build service or extension host. Unsaved documents are held in the current browser session; save them before switching interfaces or signing out.

## Organize the ErvisiOS desktop

Desktop interaction is specific to ErvisiOS:

- Click an icon once to select it and double-click to open it. Ctrl-click toggles additional icons; Shift-click selects a range.
- Hold the left mouse button on empty desktop space and drag to draw a selection rectangle. Ctrl or Shift adds its icons to the current selection.
- Drag an icon to a new location to snap it to the desktop grid. If several icons are selected, they move together. Existing icons are repositioned when needed so that shortcuts do not overlap.
- **Ctrl+A** selects desktop icons, the arrow keys navigate them and **Esc** clears selection.
- Drag shortcuts from Start onto the desktop or into a virtual folder. Existing folder and shortcut context-menu actions remain available.

Icon positions are saved with the account's desktop preferences and survive reloads. A smaller screen reflows icons into visible cells without overwriting the layout saved for a larger screen. Desktop folders continue to organize application shortcuts only; they do not create directories on Ubuntu, Windows or a network share.

## Connect network storage with SFTP or SMB

In either file-manager interface, choose **Add network folder**, select the protocol, enter the connection details, then use **Test connection** and **Save and connect**. Saved connections appear under Remote in File Explorer.

### SFTP

Enter a host, port (normally 22), username and password or private key. A private key can include its passphrase. The optional start path selects the initial remote directory; leaving it empty uses the remote account's login directory.

The first connection shows the SSH server's fingerprint. Compare it with a trusted fingerprint for that server, then choose **Trust this fingerprint and test**. A changed host key requires a new explicit trust decision. Testing an unknown key stops before SSH authentication.

### SMB network folders

Choose **SMB (Windows / network folder)** and enter a UNC or SMB URL, for example:

```text
\\storage.example.net\backups\projects
smb://storage.example.net/backups/projects
```

Both examples identify host `storage.example.net`, share `backups` and initial folder `/projects` inside that share. Enter the account's username and password; use the optional domain when the SMB server requires it. SMB normally uses port 445. These are generic examples, not live storage-provider credentials or a claim of testing a particular provider.

The connection is made **from the Ervisio server**. That server must be able to reach the network target. Ervisio exposes the saved connection inside its file manager; it does not create an operating-system mount, a Windows drive letter or a new local server directory.

### Files, transfers and saved credentials

Browse directories, create files/folders, rename, edit supported text files, upload, download, and copy or move through the file manager's existing controls and transfer queue. Split view can show the local server on one side and network storage on the other. The transfer runs through the Ervisio server, including transfers between network connections.

Connections belong to the signed-in account. Passwords, private keys and passphrases are stored in that account's server-side connection store, sealed at rest; subsequent API responses expose only whether a secret is present. Credentials are not saved in desktop preferences. The account and the server administrator can still access that account's stored connection material.

Network permissions are those granted by the remote server. Unlocking local administrator rights does not elevate a network account. Remote deletion is permanent and asks for confirmation; failures are reported. Forgetting a connection removes its saved settings without deleting remote files.

Current limits:

- SFTP and SMB have connection forms. The backend also has FTP(S) and WebDAV adapters, but their setup forms are not exposed by this update.
- Remote permissions/ownership editing and local **Open in Terminal** are unavailable for network folders. Open an SSH terminal separately when shell access to another machine is required.
- Folder downloads as browser archives are not provided. Use copy/transfer between locations or download individual files.
- Remote search filters the loaded directory; it does not recursively search the entire remote server. Remote image thumbnails are not generated by the local thumbnail service.
- Automated tests use local fixtures and an isolated SFTP server. They do not establish compatibility with a specific live Hetzner account or every SMB appliance.

## Fifteen proposed backend improvements, in priority order

The following items are recommendations for future work. They are not features delivered by this QoL update; several extend existing modules into a more complete administration workflow.

1. **Automatic offsite backups with restore tests.** Back up databases, application files and container volumes to saved network storage, with retention rules and a clear way to verify a restore.
2. **Configuration history and rollback.** Show the exact before/after diff, validate a configuration before applying it and restore the last working revision.
3. **Guided application stacks.** Combine Docker Compose, environment variables, persistent volumes and Caddy domains/TLS into one deployment flow with an explicit port and storage summary.
4. **A health overview for each stack.** Bring service status, container health, HTTP checks, open ports and dependencies together so a failed application is easy to diagnose.
5. **Actionable alerts.** Notify on unavailable services, nearly full disks, failed backups, resource pressure and certificate expiry, with deduplication and direct links to the affected component.
6. **Correlated logs.** Save filters for an application and view its container, service and proxy logs together around the same event.
7. **A job scheduler with history.** Schedule maintenance commands and recurring tasks, record exit status/output, prevent overlapping runs and support cancellation.
8. **Scoped secrets management.** Store deployment secrets separately from configuration files, control which application/plugin may use them and support rotation without exposing values in logs.
9. **Git-based deployments.** Select a repository and revision, run the build/deploy steps, check application health and offer rollback to the previous successful deployment.
10. **Access and permissions review.** Summarize users, groups, SSH keys, administrator access, active sessions and plugin permissions, with clear revocation actions.
11. **Reusable remote targets.** Let backups, scheduled jobs and deployment workflows reuse saved storage targets, with connection checks and destination capacity information.
12. **A shared server inventory.** Organize paired servers and remote environments, surface the selected host clearly and compare pending updates and health across them.
13. **Per-application resource budgets.** Connect CPU, memory, storage and network usage to application stacks, with configurable limits and capacity trends.
14. **Database maintenance workflows.** Provide guided backup/restore, connection checks and controlled migrations alongside the relevant application and deployment history.
15. **Planned maintenance with preflight checks.** Coordinate update windows and check disk space, package locks, firewall reachability, proxy validation and recovery readiness before disruptive work.

See [ErvisiOS](ERVISIOS.md) for interface selection and the existing shell/plugin contracts, and [remote API](api/remote.md) for connection and transfer details.
