# Changelog

## [0.1.0] - 2026-10-01

First public release. Early software: tested by hand on Arch Linux, other distributions are untested.

### Added
- Sign-in with Linux accounts through PAM. Root login is off by default.
- Administrator rights through `sudo`, with a configurable unlock time (including until sign-out).
- Overview: live metrics, activity chart, alerts, custom action buttons, editable widget layout, refresh rate from 1 second to 10 minutes.
- Terminal: persistent sessions (local, root, SSH), split panes, command palette, focus mode, saved commands.
- Files: tabs, split view, grid and list, previews, permissions editor, trash, uploads and downloads.
- Logs: journal, kernel, services and log files, level filters, histogram, live follow, file watchers with notifications.
- Services: systemd units by state and purpose, start/stop/restart/enable, unit overrides, dependencies.
- Software: updates, search, installed packages and history for pacman (with AUR listing), apt, dnf, zypper and Flatpak; scheduled updates.
- Users: accounts, groups, admin rights, passwords, SSH keys and login sessions.
- Plugins: sandboxed plugin pages and widgets, signed plugins, Docker example plugin.
- Settings: nine themes, custom themes, colour modes (per section, distro colour, monochrome), English and Italian.
- Self-update from signed GitHub releases with automatic rollback.
