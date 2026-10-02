# Changes from the `jobs` agent (for CHANGELOG.md, "Unreleased")

The same text is already in CHANGELOG.md under "Unreleased". Branch `feat/jobs`.

- Background jobs for plugins: `capabilities.jobs` in the manifest, job instances created with `sdk.api.jobs.*`, schedules (interval of at least a minute, or times of the day), runs as the creating user, administrator approval for jobs with root steps, history with logs (`docs/api/jobs.md`).
- Webhooks `POST /hooks/<plugin>/<token>` for job instances (unauthenticated, hashed 32-byte tokens, rate limited, revocable).
- Notification channels in Settings (email, Telegram, webhook, ntfy, Gotify), `sdk.api.notify` with `capabilities.notify`, Overview alerts, Ervisio updates and job failures sent to the channels (`docs/api/notify.md`).
- Settings pages Notification channels and Plugin jobs; the plugin install dialog lists the new permissions.
- `ervisiod --dev-state-dir`.
