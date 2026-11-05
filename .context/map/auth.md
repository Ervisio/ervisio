# Auth and sessions
Purpose: sign-in (password via PAM/LogonUser, SSH key), cookie sessions, CSRF/origin checks and admin unlock via sudo.

## Entry points and public contracts
- `server/internal/server/server.go:Handler`: routes; POST auth routes wrapped by `csrf` (public) or `authed(csrfS(...))` (session).
- `server/internal/server/auth.go`: `handleLogin`, `handleLogout`, `handleSession`, `handleUnlock`, `handleLock`, `checkCSRF`, `originAllowed`, `createSession`, `secureCookies`, `info`.
- `server/internal/server/authkey.go`: `handleChallenge`, `handleLoginKey`, `keyAuthorized`, `keyFailMinDelay` (1 s).
- `server/internal/server/session.go`: `Session`, `store` (add/get/expire/forgetIfIdle), `tokenKey`; `revalidate.go`: `revalidate`, `revalidateAll`.
- `server/internal/server/ratelimit.go`: `limiter.begin/done`, `limiterKey`; `allow.go:signInAllowed`; `proxy.go:realClientIP`.
- `server/internal/pam`: `Authenticate`, `CheckAccount`, `OpenSession`, `Service` (`ervisio` else `login`), errors `ErrAuth`/`ErrAccount`. Windows: `pam_windows.go` (LogonUser), `s4u_windows.go:AuthenticateS4U`, `admin_windows.go:AdminToken`.
- `server/internal/account`: `Lookup`, `ValidName`, `SameUser`, `LostGroup`, `ShellAllowed`.
- `server/internal/sshauth`: `challenge.go` (`Store.Issue/Take`, `Message`), `verify.go`, `authkeys.go:FindKey`, `keyfiles.go:ReadAuthorizedKeys`.
- `server/internal/winsec/winsec.go`: Windows DACL/owner inspection (used by Windows key-file checks), SID helpers.
- `server/internal/bridge/bridge.go:StartAdmin`: sudo-based unlock.
- Web: `web/src/api/session.tsx` (`SessionProvider`, `useSession`), `api/http.ts` (CSRF header, `authEvents`), `api/unlock.ts`, `shell/UnlockHost.tsx`, `pages/Login.tsx`, `pages/KeyStep.tsx`, `pages/RequireAuth.tsx`, `api/recentUsers.ts`, `auth/sshkey/` (`signin.ts`, `keys.ts`, `errors.ts`).
- Contract doc: `docs/api/auth.md` (error codes, every endpoint, key-file rules, Windows S4U).

## Flow
- Password: reserve limiter attempt (counts as failure until success) -> `pamSlot` (8 global) -> `pam.Authenticate` -> `account.Lookup` -> root/shell/allowlist checks -> `createSession` starts user bridge -> cookie.
- Key: `challenge` (nonce 60 s, single use, bound to user+client IP+host) -> browser signs `ervisio-ssh-auth-v1\nhost\nuser\nnonce` -> `login-key` verifies, reads authorized_keys, PAM account check only (no authenticate).
- Unlock: `handleUnlock` revalidates account, then `bridge.StartAdmin` runs `sudo -S -k` (password) or `sudo -n -k` (empty password) -> root bridge stored in `sess.root`; root accounts skip unlock.
- Browser: any 401 outside login/login-key/unlock fires `authEvents` -> session cleared -> `/login`. `needs_admin` -> `requestUnlock` -> `UnlockHost` dialog; key sessions first try `unlock('')` silently.
- Cookie `ervisio_session`: 256-bit random token, store keyed by SHA-256 of token; HttpOnly, SameSite=Strict, Secure per `secureCookies`.
- `revalidateAll` every 60 s: account removed/uid/shell/groups/shadow/expiry, key still authorized for key sessions; ends session and closes WS with 1008.

## Invariants
- Every POST needs `X-Requested-With: ervisio` (legacy `LinuxAdmin` value still accepted) and an `Origin`, if present, in `originAllowed`; session is checked before CSRF on session routes.
- Password failures never reveal why: PAM account refusal, shell, uid 0, allowlist all return generic `errBadCredentials`. Exception: key path returns `not_allowed` only after signature and key listing verified.
- `root` + `allow_root=false` is refused before any password check (leaks config only).
- Key failures answered no sooner than 1 s and share the password limiter and PAM slots; nonce burnt on first attempt.
- authorized_keys read with the user's fsuid and sshd StrictModes rules; `command=` and certificates refused.
- Never feed PAM an empty password: empty unlock password means `sudo -n`, not counted as a failed attempt.
- Private key and passphrase never leave the browser; client refuses to sign a challenge it did not build.
- Unlock must revalidate the account (locked/expired accounts must not gain root through a live session).
- `sessionLifetime`: 24 h absolute, or `session.timeout` if longer and "remember".
- Windows: after a failed/refused sign-in call `forgetIfIdle` so no logon token lingers.

## Gotchas
- Windows: `account.ShellAllowed` always true; no libpam; LogonUser (password) or S4U (key, needs SYSTEM/SeTcbPrivilege, no network creds). Key files: `%USERPROFILE%\.ssh` plus `administrators_authorized_keys`; `sshd_config` not read. Docs say not verified on a real Windows host.
- Linux key sessions survive `usermod -L` (as sshd); `from=` trusts `X-Forwarded-For` from trusted proxies, and loopback is a trusted proxy by default.
- Key sessions never give the server a password; admin unlock needs NOPASSWD or the password typed in the dialog.
- `--dev` only allows the daemon user, relaxes Secure cookie and accepts the Vite origin; `--dev-insecure-noauth` token route `/api/dev/noauth` is registered only in dev.
- Failed passwords count toward `pam_faillock`; an attacker who knows a user name can lock the account (documented).
- Unlock failure with wrong password is 400 `invalid`, not 401, so the UI does not treat it as sign-out (`http.ts` exempts the unlock path).
- Sign-in page user tiles live in localStorage only after opt-in (`ervisio.rememberUsers`); `PublicHost` is cached in `ervisio.host`.

## Commands and references
- `cd server && go test ./internal/server ./internal/sshauth ./internal/pam ./internal/account`; tests: `authhardening_test.go`, `authkey_test.go`, `session_test.go`, `revalidate_test.go`.
- `cd web && npm test`.
- `docs/api/auth.md`; `docs/SECURITY-REVIEW.md` sections "Addendum 2026-10-02: sign-in with an SSH key" and "SSH key sign-in, allowlist and HTTP mode review" (still shows old "linuxadmin" names and stale line numbers).
Related: map/server.md, map/web.md, map/remote-hosts.md
