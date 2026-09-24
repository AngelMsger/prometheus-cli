# Agent Guide

This file orients coding agents (Claude Code and others) working in this
repository. It is intentionally short.

## Start here

1. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) — project layout, the build/test/
   lint/docs commands, the coding conventions, and the commit/PR expectations
   every change must follow.
2. Then read, only as the task needs them, the docs under [`docs/`](docs/):
   [`technical-design.md`](docs/technical-design.md) (architecture, the
   `internal/` and `pkg/` packages, and the documented divergences — read before
   changing core behavior), [`installation.md`](docs/installation.md)
   (install / setup / distribution UX), [`read-only-mode.md`](docs/read-only-mode.md)
   (the write-safety posture), and [`releasing.md`](docs/releasing.md)
   (versioning, tagging, the release/CI workflows — read before cutting a
   release or touching `.github/workflows/`).

## What this is

`prometheus-cli` is an agent-facing CLI for the Prometheus HTTP API: run PromQL,
discover the metrics/labels/series behind a query, and read scrape targets,
rules, alerts and server status. It is a Go + Cobra CLI that mirrors the
architecture of the sibling `openobserve-cli` / `jenkins-cli` / `confluence-cli`
/ `bitbucket-cli`.

## Layout

- `cmd/prometheus-cli` — entry point; `cmd/gen-docs` — CLI reference generator.
- `internal/app` — one file per noun (query, series, metadata, target, rule,
  alert, status, admin, auth, config, doctor, skill); `root.go` assembles the
  tree; `context.go` holds the shared `appState`.
- `pkg/apiclient` — the Prometheus HTTP surface and normalized models.
- `pkg/errors` — the `CLIError` model + exit-code map (0–11).
- `pkg/timeutil` — human time expressions → Prometheus instants, and the step
  derivation that keeps a range query inside the server's point ceiling.
- `internal/output` — JSON / table / ndjson rendering, `{items,next,has_more}`.
- `internal/config`, `internal/auth` — layered loader + keychain resolution.
- `skills/prometheus` — the companion Skill, embedded into the binary.

## Ground rules

- Run `make test` and `make build` before claiming a change is complete; run
  `make e2e` for anything touching a command, an output shape or a write gate.
- Credential fallback tests assert `0600` only on non-Windows platforms.
  Windows tests must verify DPAPI-encrypted on-disk data and a store round trip;
  Windows `FileMode` does not represent POSIX access permissions.
- stdout is data only; errors / notices / `--verbose` go to stderr.
- Keep query and monitoring guidance in the companion Skill: discover before
  querying, bound every window, report the step alongside the numbers, and never
  present an empty result as a healthy one. Execute shell recovery examples with
  failing stubs when editing them.
- Never commit credentials, `.env`, or build artifacts.

## Domain rules that are easy to get wrong

These are the places where an "obvious" change would be wrong. They are
enforced by tests; read the reason before altering them.

- **An empty result is a valid answer, and a dangerous one.** Prometheus answers
  an unmatched query with `{"status":"success","data":{"result":[]}}`. Never
  turn that into an error. But never let a misconfiguration produce one either:
  a 200 without the envelope is `NOT_PROMETHEUS_API`, and a missing `--tenant`
  on a multi-tenant backend silently empties every result — which is why the
  tenant is a distributed service preset, not a per-person setting.
- **`none` is a real auth scheme, and the default.** Prometheus has no accounts.
  Nothing may require a credential, a keychain read, or a login for it.
- **PromQL failures are usage errors.** `bad_data` and `execution` classify as
  `usage` (exit 2), not `server`; retrying them unchanged cannot help.
- **`--step` is optional on purpose.** The sibling `openobserve-cli` requires it;
  here the CLI derives and clamps it because Prometheus rejects any range query
  over 11,000 points per series. The divergence, its reason and its tests are in
  `pkg/timeutil/step.go` and `docs/technical-design.md` — do not "align" it away.
- **A `--dry-run` plan must share its request builder with the real call.** A
  preview that is constructed separately can drift from what gets sent.
- **`--yes` never prompts.** An agent or CI run has no terminal; a missing
  confirmation is a structured usage error, not a hang.

## Discoverability — no dead-end inputs

**Every identifier a command accepts as input must be discoverable through
another command in this CLI.** A metric name → `metadata list` or
`labels values __name__`. A label value → `labels values <label>`. A selector →
`series list`. A rule group or file → `rule list`. A scrape pool → `target list`.
A context → `config contexts`. When you add a command or flag that takes a new
kind of input, also provide (or point its error `next_steps` at) the command
that lists values of that kind.

## When extending

New write commands must: add `--dry-run` (emitting a plan built from the same
request builder as the call), require `--yes` when destructive, route through
`apiclient.NewReadOnly` (override the new method to return `READONLY_BLOCKED`),
and keep the `{items,next,has_more}` + structured-error contract.

**Keep the companion Skill in sync — it is the agent-facing source of truth.**
Any new command, subcommand, flag, or alias must be reflected in the embedded
Skill ([`skills/prometheus/`](skills/prometheus/): the `SKILL.md` `## Commands`
list and the relevant `references/` file) **in the same commit**. Agents read the
Skill instead of `--help`, so a capability it omits effectively does not exist
for them; a flag whose help text points at another command must have that command
listed in the Skill, and no Skill claim may contradict the code.

## Team setup contract

Keep service presets separate from personal credentials. `config set-context`
must resolve the named destination (including a new one), ignore personal
variables before scheme inference, remain offline and credential-store-free,
and share one merge for dry-run and execution. Preserve other contexts, personal
usernames and shared defaults. The tenant id is a service field, not an
identity. Login must persist its identity as well as its secret and reject a
different complete service URL before credential writes; a `none` context has
nothing to store and must say so rather than writing an empty secret. Use the
common acquisition guide in all prompt styles and missing-credential recovery;
never request a guide URL with credentials. Cover a fresh config reload,
conflict/idempotent setup, and partial persistence failures when changing this
flow. The canonical behavior is in the installation guide's team setup section.

## Credential reuse

Keep `auth reuse` separate from public service setup. Match complete URLs and
provider scope before credential access, preserve configured destination identities,
verify native credentials before associating missing identity, and retain operational
failures. Never copy secrets, infer identity from environment variables or activate
a context. Cover dry-run, ambiguity, scope mismatch, concurrent edits and fresh-load
credential resolution. Native self-configuration follows the existing read-only exception.
