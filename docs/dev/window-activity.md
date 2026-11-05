# Desktop window activity

ErvisiOS keeps minimized windows mounted so drafts, selections, scroll positions and
component state survive restoration. `WindowActivityProvider active={!window.minimized}`
provides each window's activity independently. Nested providers cannot reactivate a
paused ancestor. `useWindowActivity()` also accounts for browser tab visibility.

`usePolling` removes its interval and refresh-event listener while inactive, then
refreshes immediately on restoration. `useRefreshInterval` remains the numeric saved
preference; an inactive window never converts it to zero (which would create a busy
timer). `useActivityExternalStore` unsubscribes hidden UI consumers and retains their
last snapshot until they become active again.

Overview metrics close their shared stream as soon as no visible consumer remains.
Logs pause their UI follow stream, histogram and sources polling. File listings/search
and places subscriptions, service logs, Settings pollers and terminal session-list
reads pause independently. Restoring a view reconnects/reloads its current data.

Minimizing never cancels an installation, file operation, server job, AI request or
actual PTY. The terminal attachment remains connected so root-session lifetime and
shell output are preserved; hidden cursor blinking and visual observers are paused.
App-wide sign-in, alerts and notification work remain independent of any one window.

This is cooperative suspension of unnecessary UI work, not a fixed CPU or RAM quota.
Mounted state still occupies memory, and explicit background work or a busy shell can
consume resources. Browsers do not expose enforceable per-component CPU/RAM limits.
