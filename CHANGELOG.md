# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/AngelMsger/prometheus-cli/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/AngelMsger/prometheus-cli/releases/tag/v0.1.0
