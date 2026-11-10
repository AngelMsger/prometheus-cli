# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-11-10

### Added

- `pkg/apiclient` exports `Reader` and `Writer`; `Client` is now their union,
  with the same method set and signatures as before.
- `pkg/errors` re-exports agent-cli-kit's `RedirectGuide`, which names the
  setting and next steps a redirect's error gives.

### Changed

- The read-only wrapper keeps the inner client only as a `Reader` and blocks
  each `Writer` method (the TSDB admin API) itself, so a write added without a
  blocking override fails to compile instead of reaching Prometheus in a
  read-only session. No CLI behavior, output or error changes.
- `READONLY_BLOCKED` uses agent-cli-kit v0.6.0's shared wording: the message
  reads `operation "delete series" blocked: read-only mode is enabled`, the
  hint says read-only mode is the user's setting, and the next steps are
  `--dry-run` and, only for a write the user authorized, `--allow-writes`. They
  no longer suggest unsetting `PROMETHEUS_CLI_READ_ONLY` or
  `defaults.read_only`.
- Prompts (`auth login`, `config init`) use agent-cli-kit's
  `terminal.Prompter`; their wording is unchanged.

### Fixed

- `pkg/transport`, for a library caller that passes its own HTTP client
  (`Options.Doer`): a read with a body (a `RetryMethods` verb, or a request
  marked with agent-cli-kit's `transport.AsRead`) that the client turned into
  a GET on a 301, 302 or 303 is answered by that redirect, not by the GET's
  page, and a transient failure of that page no longer retries the read. This
  is agent-cli-kit v0.8.1. The CLI's own client never follows such a redirect,
  so the CLI's behavior is unchanged.
- A query behind a 307 or 308 redirect (for example http to https) is sent
  again with its form, and a transient failure of a query is retried, as for
  any read. The query endpoints are posted, and agent-cli-kit v0.8.0 follows
  and retries a POST only when the CLI marks it as a read
  (`transport.AsRead`), which the query, series and labels calls now do.
- A redirect the client does not follow is reported as itself: `HTTP_<STATUS>`
  (server, exit 9, not retryable) with a hint naming where it points (the
  scheme and host only) and to check `--base-url`, the server root, with
  agent-cli-kit v0.9.0's family redirect rule (`CLIError.WithRedirect`); a
  redirect without a `Location` gets a hint that the same request gets the
  same answer. That is a query's 301, 302 or 303, which would turn it into a
  GET without the query; any redirect of an admin write, which is never sent
  again; and a redirect Go cannot follow, such as one without a `Location`. An
  admin write's hint adds that the write may still have applied behind the
  redirect: `snapshot` points at the snapshots directory, `clean-tombstones`
  says repeating it is safe, and `delete-series` stays an unknown outcome
  (Prometheus can keep listing a deleted series, so no read settles it); a
  repeat needs the user's confirmation, and the only command offered is a
  `--dry-run` with the window fixed, ending at the delete's end or the first
  attempt, whichever is earlier (from the epoch for a delete without a start);
  the hint says when that leaves part of the window out, or when no repeat is
  offered. **Error codes change for these responses:** 0.5.0 followed a
  query's redirect as a GET and failed on the page behind it with a parse
  error (exit 10): `DECODE` for an HTML login page, `NOT_PROMETHEUS_API` for
  an empty body or JSON without the Prometheus envelope. A redirect without a
  `Location` failed the same way, or, if its body carried a Prometheus
  envelope, succeeded or took the envelope's error. A 307 or 308 sent an admin
  write again. A script that took exit 10 as a wrong URL now gets exit 9 with
  `retryable: false` for a redirect.
- A read that loops through 10 redirects reports the last one
  (`HTTP_FOUND` or the redirect's own code, server, exit 9, not retryable,
  with the redirect hint). 0.5.0 reported `NETWORK` (exit 8, retryable, a
  DNS/TLS/timeout hint), although nothing retried it.
- `doctor`'s `connectivity` check reports a redirect with status
  `redirected` instead of `unreachable`, and its `detail` adds the redirect
  hint (where it points, and to check `--base-url`). 0.5.0 showed only
  "Prometheus returned HTTP 302". The status and detail rules come from
  agent-cli-kit's `doctor` package, shared by the family.
- Update the companion Skill to `0.1.6`: a redirect is not retryable,
  `doctor`'s `redirected` status, and the scripted error example reads
  `retryable` before retrying an exit 9.
- Mask passwords embedded in the server URL in `--verbose` request logs, with
  agent-cli-kit v0.6.0's transport.

- `--verbose` now logs through agent-cli-kit's transport: one line per request
  to the server, `METHOD URL -> STATUS` or `METHOD URL -> error: ...`, on
  stderr, for every command and now also the login check. It used to print
  `> METHOD URL` before each request. The parse-error guidance no longer says
  `--verbose` shows raw content; it shows each request's URL and status. The
  release check, which asks for a newer version, is not logged.
- `query instant` and `query range` reject a success reply whose data names no
  `resultType` or carries no `result`, with `DECODE` (parse, exit 10). Such a
  reply, from a proxy or a partly compatible backend, used to read as an empty
  result. An empty or `null` vector or matrix is still an empty answer.

## [0.5.0] - 2026-10-27

### Changed

- `auth reuse` looks past the destination's own scheme. A context without a
  working login (its token or password is absent, rejected or incomplete)
  adopts a password or bearer token already verified for the same service URL,
  path and tenant, under either scheme: another context's, or one stored for
  this service that no context points at any more, paired with the usernames
  the service's contexts record. Contexts under the destination's own scheme
  still come first. A working login is never replaced automatically, not even
  by an explicit selection. A username under `bearer` is now only a label: such
  a context is checked instead of being reported `unchanged`, and a context on
  the same stored token no longer makes the command fail when the keychain
  shows that token is gone. Checks the command did not make before run within
  ten seconds (or `--timeout`), adopt only when the keychain itself showed the
  destination has no working login, and otherwise leave the earlier answer. A
  scheme supplied by flag, environment, `.env` or a credential variable keeps
  reuse within it.
  A `none` context is still neither a source nor a destination. The decision
  now comes from agent-cli-kit's shared `reuse` package.
- The `auth reuse` result adds `scheme`, `username` and `next_steps`;
  `AUTH_REUSE_AMBIGUOUS` adds `details.candidates`.
- `config set-context`, `auth guide`, missing-credential errors, `doctor` and
  HTTP 401 errors list `auth reuse` before any sign-in step when the
  configuration file shows that a stored login could apply.
- Update the companion Skill to `0.1.5` for `auth reuse`.

### Added

- `auth reuse --from-scheme <scheme>` selects the credential stored for the
  service under that scheme; with `--from-context`, paired with that context's
  username.

## [0.4.1] - 2026-10-19

### Changed

- The `skill` commands, the Skill notice and `doctor` read the Skill version,
  load state, installed copies and agent directories through agent-cli-kit's
  `skill` package instead of a private copy. Output is unchanged.

### Fixed

- Login storage failures preserve any known saved backend and direct recovery
  to the host credential store without recording an unconfirmed identity.
  Logout failures report incomplete removal instead of generic recovery.

## [0.4.0] - 2026-10-12

### Added

- `config delete-context <name>` removes a context. Its stored secret is removed
  too, after the config file is written, unless another context on the same
  server URL (host and path) and authentication scheme still uses it; a `none`
  context has no secret and leaves the credential store alone. Deleting the
  current context makes the first remaining one current; the only context
  cannot be deleted (`LAST_CONTEXT`).

### Changed

- `config get-contexts` lists contexts, matching the rest of the family; its
  output is unchanged. `config contexts` keeps working as a hidden command.
  Hints and next steps (`UNKNOWN_CONTEXT`, `auth reuse`) and the
  `--from-context` help now name `config get-contexts`.
- A new context name from `config init --context` or the wizard's "add" prompt
  is saved trimmed and in lower case (`--context Prod` writes `prod`), as
  `config set-context` already did. A blank `--context` fails with
  `CTX_NAME_EMPTY`. Existing names keep their spelling and still match in any
  case.
- Update the companion Skill to `0.1.4` for the context commands.
- Layered configuration, context selection, the config file write and
  `config set-context` planning now come from agent-cli-kit's `config`
  package. Commands, output, error codes, hints and the `config.yaml` format
  are unchanged.
- Credential validation, header encoding, keychain lookup and the credential
  recovery errors now come from agent-cli-kit's `auth` package. Messages, error
  codes, the tenant header and stored credentials are unchanged.
- Output rendering, argv correction, the release check, the credential store,
  and the code behind `pkg/errors` and `pkg/transport` now come from the shared
  [agent-cli-kit](https://github.com/AngelMsger/agent-cli-kit) module. Commands,
  output, exit codes, hints and stored credentials are unchanged, and
  `pkg/errors` and `pkg/transport` keep their exported names.

### Fixed

- Never raise the macOS "Reset To Defaults" keychain dialog. When the user's
  HOME had no default keychain (common in agent sandboxes), saving a credential
  made macOS offer to reset the default keychain, then failed with `exit status
  154`. The credential store now checks for a default keychain first, once per
  store, and uses its protected file fallback when none exists. Ported from
  `openobserve-cli`.

## [0.3.0] - 2026-10-06

### Added

- **NDJSON continuation notices.** With `--format ndjson`, a `rule list` page
  that has more now prints one compact line on stderr after its rows:
  `{"_notice":{"pagination":{"next":…,"has_more":true},"next_steps":[…]}}`.
  Pass `next` as `--cursor`. The notice is kept for a filtered page with no
  rows and under `--fields`; stdout stays item-only, JSON and table output are
  unchanged, and completed or unpaginated results print none. The renderer is
  the one the maintained CLI family shares: continuation guidance names the
  command's actual cursor or offset flag and never follows a failed row write.
  Previously an NDJSON page gave no sign that more rule groups existed.
- **Discovery advisories.** Server warnings and info annotations on non-query
  endpoints — a truncated `series list`, `labels list` or `labels values` among
  them — are preserved as a structured `_notice.api_advisories` record on
  stderr in every format, with `next_steps`. `has_more: false` means no cursor
  page follows, not that a discovery limit returned every match.
- **`pkg/apiclient`: `OnAdvisory`.** `Config.OnAdvisory` and
  `BuildParams.OnAdvisory` are optional callbacks that receive an `Advisory`
  (endpoint path, warnings, infos) for each successful response, so library
  consumers can read the same metadata without any change to the existing
  client method signatures.

### Changed

- **Time-window flags follow the family contract.** `--since` is now mutually
  exclusive with `--from`/`--to`, and `--to` requires `--from`, on every command
  that takes a window: `query range`, `query exemplars`, `series list`,
  `labels list`, `labels values` and `admin delete-series`. A violation is
  `BAD_TIME_RANGE` (usage, exit 2) and sends no request. Previously `--since`
  silently won over an explicit `--from`/`--to` — on `admin delete-series`,
  deleting a window other than the one written — and `--to` alone was reported
  as "no time range given". Scripts that passed both forms must drop one.
- **`pkg/timeutil`: `Range.Resolve` enforces the same rules.** The exported
  shape is unchanged, but a `Range` that sets `Since` together with `From` or
  `To`, or `To` without `From`, now returns an error instead of resolving.
  Library consumers that relied on `Since` taking precedence must clear the
  other fields.

### Fixed

- Reject empty HTTP 200 responses and unexpected 204 responses as
  `NOT_PROMETHEUS_API`; retain valid bodyless delete and cleanup responses.
- Reject negative limits (`BAD_LIMIT`) and nonpositive explicit query timeouts
  (`BAD_QUERY_TIMEOUT`) before sending requests instead of silently dropping
  those bounds.
- Merge float and native-histogram samples chronologically, and preserve the
  `value` field for empty string samples without adding it to histogram samples.
- Keep a query's server warnings and infos visible under `--fields`: a
  projected JSON or table result now re-emits them as the `query_advisories`
  stderr notice, as `--format ndjson` already did. The notice gains `next_steps`
  naming the narrower-query and `--limit` recovery path.

### Skill

- Update the companion Skill to 0.1.3 with discovery completeness, pagination,
  bounds and time-window guidance, and list `auth reuse` in its `## Commands`
  block; add process-level regression coverage for response integrity.

### Documentation

- Add the family "Related" section to the README and link all six sibling CLIs
  from the landing-page footer, including `wecom-calendar-cli`.
- Correct the landing page's install card: `skill install` detects thirteen
  agent locations (Claude Code, Codex, Cursor and ten more), not two.
- List `auth reuse` in the README command table.

## [0.2.0] - 2026-09-24
### Added

- Add `auth reuse` to verify and associate an existing matching login with a team
  context without another login or copying secrets; include dry-run and explicit
  source selection for ambiguous identities.


## [0.1.0] - 2026-09-22

First release: an agent-facing CLI for the Prometheus HTTP API, aligned with the
sibling `openobserve-cli` / `jenkins-cli` / `confluence-cli` / `bitbucket-cli` /
`jira-cli` contracts.

### Added

- **Query** — `query instant`, `query range`, `query exemplars`,
  `query format` and `query parse`. Expressions can be passed inline, from a
  file (`--query @path`) or from stdin (`--query @-`), and are sent as form
  POSTs so a long expression is never truncated in a URL. `--limit`,
  `--query-timeout` and `--stats` map to the server's own options.
- **Automatic step resolution.** `query range` makes `--step` optional: with no
  step the CLI derives a round resolution for the window, and a step that would
  exceed Prometheus' 11,000-point ceiling is raised rather than sent. Every
  result reports the step, its source (`flag` / `derived` / `clamped`) and the
  resulting point count.
- **Normalized samples.** Prometheus' positional `[timestamp, "value"]` tuples
  come back as named fields with an RFC3339 instant beside the raw epoch.
  Values stay strings so `NaN` and `+Inf` survive; native histogram samples are
  preserved verbatim.
- **Discovery** — `series list`, `labels list`, `labels values`,
  `metadata list` and `metadata targets`, each with the shared
  `--since` / `--from` / `--to` window and a bounded default `--limit`.
- **Operations** — `target list` (active and dropped in one list, with
  `--unhealthy`), `rule list` (rules flattened out of their groups, with
  `--failing`, `--exclude-alerts` and group paging), `alert list`,
  `alert managers`, and `status config|flags|runtimeinfo|buildinfo|tsdb|wal-replay|notifications`.
- **TSDB admin writes** — `admin delete-series`, `admin clean-tombstones` and
  `admin snapshot`, each with `--dry-run`, read-only gating, and `--yes` on the
  two destructive ones. A 404 on an admin path is reported as
  `ADMIN_API_DISABLED` rather than a bare not-found.
- **`none` as a first-class auth scheme**, the default: a plain Prometheus needs
  no credential, no keychain access and no login. `basic` and `bearer` cover
  proxied and hosted deployments, and `--tenant` sends `X-Scope-OrgID` for
  Cortex, Mimir and Thanos Receive.
- **Configuration** — layered flags / env / `.env` / file / defaults with
  provenance, kubectl-style named contexts, the offline `config set-context`
  team-preset flow, and `auth guide` / `auth login` / `auth status`.
- **Agent ergonomics** — JSON by default with structured errors and stable exit
  codes, `{items, next, has_more}` listings, `--fields` projection, ndjson
  streaming, argv slip correction, the multi-context reminder, the update
  notice, and `doctor`.
- **Companion Skill** (`prometheus`) embedded in the binary, with
  `skill install|status|path|show|uninstall` and the versioned handshake.
- A `NOT_PROMETHEUS_API` error for a 200 response without the Prometheus
  envelope, so a `--base-url` pointing at a dashboard or proxy route is
  diagnosed instead of returning a plausible-looking empty result.
- Base URLs ending in `/api/v1` are trimmed back to the server root at both the
  client and the persistence boundary.

[Unreleased]: https://github.com/AngelMsger/prometheus-cli/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/AngelMsger/prometheus-cli/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/AngelMsger/prometheus-cli/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/AngelMsger/prometheus-cli/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/AngelMsger/prometheus-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/AngelMsger/prometheus-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/AngelMsger/prometheus-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/AngelMsger/prometheus-cli/releases/tag/v0.1.0
