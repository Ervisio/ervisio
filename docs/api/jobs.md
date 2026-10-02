# Background jobs and webhooks (`plugins.jobs.*`, `jobs.*`, `/hooks/…`)

Plugins normally work only while one of their pages is open. Jobs let a plugin do work in the background: poll a Git
repository, back up a volume, run a check and send a notification. A plugin **declares** what a job may do in its
manifest (`capabilities.jobs`); at run time it **creates job instances** from those declarations, with a schedule or a
webhook URL. The daemon runs them, also when nobody is signed in, and keeps them across restarts.

Code: `server/internal/jobs` (manager, scheduler, runner, webhooks), `server/internal/server/jobsglue.go` (RPC methods,
bridge executor, `/hooks`), the manifest part in `server/internal/modules/plugins/manifest_jobs.go`. Web: Settings ›
Plugin jobs (`web/src/sections/settings/PluginJobs.tsx`), the broker `web/src/plugins/brokerJobs.ts`. Notifications
(the `notify` step, failure alerts) are in `docs/api/notify.md`.

## The model

```
manifest capabilities.jobs      what a job may do: ordered steps over the plugin's own commands / HTTP APIs
        │
job instance (plugins.jobs.create)   which job, which params, which schedule, who owns it
        │ schedule · runNow · webhook
run                              one execution: step logs, status; the last 20 are kept
```

* **No new powers.** A step is a declared command (by name, with arguments that are either fixed or bound to job
  parameters) or a declared HTTP call. The daemon sends each step to a bridge as `plugins.exec` / `plugins.http`, so it
  passes the same checks as a call from a page: the command's arg patterns, the HTTP rules, enabled plugin, signature
  policy, `visibleTo`. A job cannot do anything the plugin could not do by hand while a page was open.
* **Run as the creator.** An instance runs as the user who created it, through a bridge started for that user. `runAs`
  in `plugins.jobs.create` must be empty or the caller.
* **No scripting.** Steps run in order. A step can be conditional on one earlier step. That is all.

## `capabilities.jobs` in manifest.json

```json
"capabilities": {
  "notify": true,
  "commands": [
    {"name":"git-fetch","argv":["git","-C","{0}","fetch"],"args":[{"pattern":"/opt/stacks/[a-z0-9_.-]+"}]},
    {"name":"git-rev","argv":["git","-C","{0}","rev-parse","{1}"],"args":[{"pattern":"/opt/stacks/[a-z0-9_.-]+"},{"pattern":"HEAD|@\\{u\\}"}]},
    {"name":"git-pull","argv":["git","-C","{0}","pull","--ff-only"],"args":[{"pattern":"/opt/stacks/[a-z0-9_.-]+"}]},
    {"name":"compose-up","argv":["docker","compose","-f","{0}/compose.yaml","up","-d"],"args":[{"pattern":"/opt/stacks/[a-z0-9_.-]+"}]}
  ],
  "http": [{"name":"docker","socket":"/var/run/docker.sock","rules":[{"methods":["POST"],"path":"/containers/create"}]}],
  "jobs": [
    {"name":"git-poll","description":"Pull a stack when its Git remote moved.","timeoutSec":600,
     "params":[{"name":"dir","pattern":"/opt/stacks/[a-z0-9_.-]+"}],
     "steps":[
       {"id":"fetch","command":"git-fetch","args":["{param.dir}"]},
       {"id":"local","command":"git-rev","args":["{param.dir}","HEAD"]},
       {"id":"remote","command":"git-rev","args":["{param.dir}","@{u}"]},
       {"id":"pull","if":{"step":"remote","when":"differs","other":"local"},"command":"git-pull","args":["{param.dir}"]},
       {"id":"up","if":{"step":"pull","when":"ok"},"command":"compose-up","args":["{param.dir}"]},
       {"id":"tell","if":{"step":"up","when":"ok"},"notify":{"title":"Updated {param.dir}","body":"Now at {step.remote.stdout}","level":"success"}}
     ]}
  ]
}
```

Strict like the rest of the manifest (unknown fields are rejected). At most 16 jobs, 16 steps and 8 params each.

**Job**: `name` (like a command name, unique), `description?`, `params?`, `steps`, `timeoutSec?` (whole run; default 300,
max 3600), `webhook?` (`{"params":[names]}`: the params a webhook call may set, see "Webhooks").

**Param**: `name` (`^[a-z][a-z0-9_]{0,23}$`), `pattern` (a regular expression the **whole** value must match, as for
command args), `maxLen?` (default 256, max 1024), `default?` (must match the pattern; a param without a default is
required), `description?`. Values never hold control characters. The pattern is checked when an instance is created,
when it is changed and at every run (a plugin update may have tightened it).

**Step**: `id` (same form as a param name, unique) and exactly one of:

* `command` + `args`: a declared, non-`pty` command. `args` has one entry per `args` slot of the command, in order.
  Each entry is text with placeholders. A fixed entry (no placeholder) is checked against the command's pattern when
  the manifest loads; an entry with placeholders is checked when the step runs, by the same code that checks `plugins.exec`
  (pattern, `allowDash`, `maxLen`).
* `http`: `{"api", "method", "path", "query"?, "headers"?, "body"?, "json"?}`. `path` and `query` take only
  `{param.x}` (the value is escaped for a URL path or query), `headers` values likewise param-only, `body` takes
  `{param.x}` and `{step.id.field}` (escaped for JSON when `json` is true, so a value cannot break out of its string).
  A fixed path is checked against the API's rules when the manifest loads, a templated one at run time.
* `notify`: `{"title", "body"?, "level"?, "link"?}`, needs `capabilities.notify: true`. Sends a notification to the
  channels (`docs/api/notify.md`), counted against the plugin's rate limit.

Other step fields: `if` (below), `continueOnError` (the failure is recorded as handled and does not fail the run, so
a later step can react to it; without it a failed step ends the run as failed).

**Placeholders**: `{param.name}`, `{step.id.stdout}`, `{step.id.stderr}`, `{step.id.exitCode}` (command steps),
`{step.id.status}`, `{step.id.body}` (HTTP steps), `{job}`, `{instance}`, `{plugin}`. Text between braces that is not
one of these (JSON, `{0}`, `@{u}`) stays as it is; a typo such as `{param.Dir}` is a manifest error. Step outputs are
the trimmed stdout / body, cut to 4096 characters; a step that did not run gives "".

**Condition** `if: {"step": id, "when": …, "other"?: id}` on an **earlier** step:

| `when` | true when |
|---|---|
| `ok` | the step ran and succeeded (exit code 0, HTTP 2xx) |
| `failed` | the step ran and failed |
| `changed` / `unchanged` | the step's output is different from / the same as in the instance's previous run (no previous run counts as changed) |
| `differs` / `same` | the step's output is different from / the same as step `other`'s output in this run |

A condition on a step that did not run (skipped) is false. A skipped step is logged as `skipped`.

**Admin flag.** It is inherited from the steps: a job needs administrator rights for a user when a step uses a command
or HTTP API declared `admin` that the user is not exempt from (`adminUnlessGroup`, or the user is root). `needsAdmin`
in an instance says so.

## Job instances

An instance is stored in `/var/lib/ervisio/jobs/instances.json` (0600; `--dev-state-dir` in dev), its runs in
`jobs/runs/<id>.json`. Fields: `id`, `plugin`, `job`, `name?`, `params`, `schedule?`, `owner` (user name, with its uid
kept to detect a recycled name), `enabled`, `disabledReason?`, `approval?`, `webhooks`, timestamps, and the hashes of
each step's last output (for `changed`).

* **Schedule** is an interval, `{"every": seconds}` (at least 60), or times of the day, `{"at":["03:30","15:00"],
  "days":[1,5]}` in the server's local time zone (`days` 0 = Sunday ... 6 = Saturday; no `days` = every day). No
  schedule: the instance runs on demand (`runNow`) or by webhook. An interval that fell due while the daemon was down
  runs once, right after the start. Times of the day that passed while it was down are not caught up.
* **One run at a time** per instance. A scheduled tick while it runs is skipped. `runNow` while it runs fails with
  `conflict`. A webhook call while it runs is queued: one run starts when the current one ends; further calls meanwhile
  get the id of the queued run. At most 4 runs execute at once on the server; the others wait (status `queued`).
* **Timeout.** The whole run is cancelled after the job's `timeoutSec` (status `timeout`); each command and HTTP call
  also keeps its own timeout.
* **History.** The last 20 runs per instance are kept, with logs: per step the last 8 KiB of stdout, 4 KiB of stderr
  (`truncated` is set), and at most 64 KiB for the whole run. Logs may hold anything a command printed, so only the
  owner and administrators can read them.
* **Plugin removed.** Instances of a plugin that is turned off, uninstalled or lost its job stay, and their runs fail
  with a message saying so (an administrator deletes them in Settings › Plugin jobs). They work again when the plugin
  is back.
* **Limits.** 200 instances, 50 per plugin, 5 webhook URLs per instance.

### Administrator approval

A job with steps that need root runs through a root bridge that the daemon starts itself, **without an interactive
unlock** (nobody is there to type a password). So:

1. Creating (or changing the params of) such an instance needs `confirmAdmin: true` **and** a caller who can administer
   the machine (root, or a member of `sudo`/`wheel`/`admin`). Without the confirmation the call fails with
   `invalid`, `data.reason = "admin_confirmation_required"`; a caller who cannot administer gets `forbidden`.
2. The instance records who approved it and when (`approval.by`, `approval.at`), plus a signature of what was approved:
   the job definition and every command and HTTP API it uses. A plugin update that changes any of them invalidates the
   approval: the instance is switched off ("An administrator has to approve it again") until its params are saved again
   with `confirmAdmin`.
3. Before every run, and every minute, the daemon checks the owner. The instance is **disabled** (with
   `disabledReason`) when the account is gone or changed uid, may no longer sign in (shell, `auth.allow_*`, `allow_root`,
   expired), or, for an admin instance, **is no longer able to administer the machine**. Switching it on again checks
   the same things.
4. The daemon runs admin steps only when it runs as root. A daemon started with `--dev` is not root: such a step fails
   with a message that says so, it never runs with fewer rights.

Steps that do not need root run on the owner's user bridge, which opens a PAM session like a sign-in does. A bridge
is kept for 5 minutes after the last run that used it (a job every minute does not open a session each time), and
stopped at shutdown. The root bridge of admin steps is kept the same way.

## Webhooks

`POST /hooks/<plugin>/<token>` runs the instance the token belongs to.

* The token is 32 random bytes, base64url (43 characters), made by `plugins.jobs.webhooks.create`, shown **once** and
  never stored: only its SHA-256 is. Lookup compares the hash of the given token against every stored hash in constant
  time. A webhook is regenerated (new token, old URL dead) or revoked from the SDK or from Settings.
* **Unauthenticated, outside `/api`, no CSRF header, no cookie**: callers are CI systems and registries, not browsers.
  The token in the path is the credential. It works behind a reverse proxy like the rest of the console (the client
  address, for the limits below, follows `web.trusted_proxies`); no origin check applies.
* The token is part of the URL, so a reverse proxy's access log holds it: treat the log like the URL. Revoke and
  regenerate when it leaks.
* **Rate limits.** Per token: a burst of 6 calls, then one per 10 seconds. Per client address: 20 misses (unknown
  tokens), then one per 3 seconds.
* **Body.** Ignored, unless the job declares `webhook.params`: then a JSON object body (`{"tag":"v2"}`) or the query string
  (`?tag=v2`) may set those params (strings or numbers). Each value must match the param's pattern, or the call fails with
  400. Everything else is ignored. At most 4 KiB of the body is read.
* **Answers**, deliberately plain: `202 {"run":"<id>"}`; `400 {"error":"invalid request"}` (a param off its pattern);
  `404 {"error":"not found"}` for an unknown or revoked token, a token of another plugin, a disabled instance, a
  plugin that is switched off; `429 {"error":"too many requests"}` with `Retry-After`. Nothing says which part was wrong.
  A `GET` on the same path is served by the web app, never by a job.

## Methods

Handled **by the daemon** (not a bridge), through the usual `POST /api/rpc`. Instances are visible to their owner and,
for the Settings page, to administrators (root, or after unlocking); everyone else gets `not_found`.

### Plugin-scoped (user level; SDK `sdk.api.jobs`)

All take `plugin` (the broker fills it in from the frame's manifest; the daemon checks it again).

| Method | Params → result |
|---|---|
| `plugins.jobs.create` | `{plugin, job, name?, params?, schedule?, runAs?, enabled?, confirmAdmin?}` → instance |
| `plugins.jobs.list` | `{plugin, job?}` → `{instances}` (the caller's own; an administrator sees all) |
| `plugins.jobs.get` | `{plugin, id}` → instance |
| `plugins.jobs.update` | `{plugin, id, name?, params?, schedule? (null clears), enabled?, confirmAdmin?}` → instance |
| `plugins.jobs.delete` | `{plugin, id}` → `{}` (cancels a run in progress, removes its runs) |
| `plugins.jobs.runNow` | `{plugin, id}` → `{run}` |
| `plugins.jobs.history` | `{plugin, id, limit?}` → `{runs}`, newest first, the run in progress first, with step logs |
| `plugins.jobs.webhooks.create` | `{plugin, id, label?}` → `{id, label?, token, path}`; `path` is `/hooks/<plugin>/<token>` |
| `plugins.jobs.webhooks.regenerate` | `{plugin, id, webhook}` → same shape, new token |
| `plugins.jobs.webhooks.revoke` | `{plugin, id, webhook}` → `{}` |

Instance: `{id, plugin, job, name, params, schedule?, owner, enabled, disabledReason?, needsAdmin, approval?: {by, at, valid},
webhooks: [{id, label?, created, lastUsed?}], created, updated, running, nextRun? (ms), last?: {id, trigger, status, started,
ended?, error?}}`. Run: `{id, instance, trigger: schedule|manual|webhook, by?, started, ended?, status: queued|running|ok|failed|timeout|cancelled,
error?, steps: [{id, kind, status: ok|failed|skipped, durationMs?, exitCode?, httpStatus?, stdout?, stderr?, error?, truncated?, admin?, handled?}]}`.

Errors: `not_found` (no such plugin, job or instance), `forbidden` (plugin not available to the caller; `runAs` another
user; a non-administrator asking for an admin job), `invalid` (params, schedule, confirmation), `conflict` (`runNow` on
a running or switched-off instance), `unavailable` (limits).

### Settings (admin level)

`jobs.list {plugin?}` (all instances), `jobs.setEnabled {id, enabled}`, `jobs.runNow {id}`, `jobs.delete {id}`,
`jobs.history {id, limit?}`, `jobs.webhooks.create {id, label?}`, `jobs.webhooks.regenerate {id, webhook}`,
`jobs.webhooks.revoke {id, webhook}`. Without administrator rights they fail with `needs_admin` (the web client
opens the unlock dialog). An administrator can act on any instance, but cannot approve a job that needs root
without `confirmAdmin`.

## Failure alerts

When an instance starts failing (a run ends `failed` or `timeout` after a good one) and when it works again, the
daemon sends one notification to the channels that subscribe to `jobs` (`docs/api/notify.md`); repeated failures
send nothing more.

## Activity log

Jobs write to the activity log (Settings › Activity log, `sdk.audit.list()`) as the instance's owner, with `origin` set to
`job <name>` for scheduled and manual runs and to `webhook` for runs started by a webhook:

* `command` and `http` entries for every step command and every HTTP call other than `GET`/`HEAD` (with `detail` naming the
  job and instance);
* `job.run` when a run ends (`ok` or `failed`, with the run id and who started it);
* `job.webhook` when a webhook call is accepted (token misses are not logged);
* `job.approve` when an administrator approves an instance that needs administrator rights.

Changes to notification channels (`notify.channel.add`, `notify.channel.change`, `notify.channel.delete`) are core entries.

## Environments

Job steps cannot target an environment yet (`docs/api/environments.md`): commands and HTTP calls of a job always run on this
machine.
