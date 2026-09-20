# Contributing to prometheus-cli

Thanks for helping improve `prometheus-cli`. This guide covers the project
layout, the everyday commands, and the conventions every change must follow.

## Prerequisites

- Go 1.24+ (the version is pinned in `go.mod`).
- `make`, `bash`, and `curl` (for the end-to-end script).

## Build, test, lint

```bash
make build      # compile to ./bin/prometheus-cli
make test       # unit + httptest integration tests
make e2e        # build, then run against the in-repo mock server (no creds)
make lint       # gofmt + go vet
make docs       # regenerate docs/cli/ from the cobra command tree
make cross      # cross-compile every platform into dist/ (release only)
```

Run `make test` and `make e2e` before claiming a change is complete. CI
(`.github/workflows/ci.yml`) runs gofmt, `go vet`, a `docs/cli/` drift check,
the unit tests and the e2e suite on every push and PR.

## Project layout

```
cmd/prometheus-cli    entry point (delegates to internal/app)
cmd/gen-docs          generates docs/cli/ from the cobra tree
internal/app          one file per noun (query, series, metadata, target, rule,
                      alert, status, admin, auth, config, doctor, skill) + root
                      wiring and the shared appState
pkg/apiclient         the Prometheus HTTP API surface and normalized models
pkg/transport         retrying HTTP client with request decorators
pkg/errors            the CLIError model and exit-code map (0–11)
pkg/timeutil          human time expressions ⇄ Prometheus instants; step derivation
pkg/constants         app name, defaults, build-time version vars
internal/output       JSON / table / ndjson rendering, {items,next,has_more}
internal/config       layered configuration + named contexts
internal/auth         credential model + OS keychain storage
internal/cliflags     argv normalization (LLM slip correction)
skills/prometheus     the companion Skill, embedded into the binary
test/mockserver       a stand-in Prometheus API for the e2e suite
```

See [docs/technical-design.md](docs/technical-design.md) for how these fit
together.

## Conventions

- **gofmt-clean.** CI fails otherwise. `make fmt` fixes it.
- **stdout is data only.** Errors, notices and `--verbose` diagnostics go to
  stderr; never print anything else to stdout.
- **Structured errors.** Every user-facing failure is a `*errors.CLIError` with a
  `category`, a stable `code`, a `hint` and `next_steps`. The category maps to an
  exit code in `pkg/errors/codes.go`. A caller's mistake (bad PromQL, a bad
  selector) is a `usage` error, not a retryable one.
- **Absorb the footguns.** Anything that would make a caller decode a positional
  tuple, convert an epoch, walk a nested document, or discover a server limit by
  hitting it belongs in `pkg/apiclient` or `pkg/timeutil`, not in the caller.
- **No dead-end inputs.** Every identifier a command accepts must be discoverable
  through another command (a metric name from `metadata list`, a label value
  from `labels values`, a rule group from `rule list`, a scrape pool from
  `target list`). When you add an input, also provide — or point its error
  `next_steps` at — the command that lists values of that kind.
- **An empty result is a contract, not a bug.** Prometheus answers an unmatched
  query with an empty result and HTTP 200. Never convert that into an error, and
  never let a misconfiguration produce one silently: that is why a 200 without
  the Prometheus envelope is `NOT_PROMETHEUS_API`.
- **Keep the CLI reference in sync.** After changing a command or flag, run
  `make docs` and commit the regenerated `docs/cli/`.
- **Update the changelog.** Add a bullet under `[Unreleased]` in
  [CHANGELOG.md](CHANGELOG.md) for any user-visible change.
- **Never commit** credentials, `.env`, `dist/`, `bin/`, or build artifacts.

### Adding a write command

The three TSDB admin commands are the only writes. A new one must:

- add `--dry-run`, emitting the would-be request through a `*Plan` helper in
  `pkg/apiclient` that **shares its request builder with the real call**, so a
  preview can never drift from what would be sent;
- require `--yes` if it destroys data, via `requireConfirmation` — which never
  prompts, so a non-interactive caller fails rather than hangs;
- get one override in `pkg/apiclient/readonly.go` returning `READONLY_BLOCKED`;
- keep the `{items, next, has_more}` and structured-error contracts.

### Keep the companion Skill in sync

The Skill is the agent-facing source of truth. Any new command, subcommand, flag
or alias must be reflected in [`skills/prometheus/`](skills/prometheus/) — the
`SKILL.md` `## Commands` list and the relevant `references/` file — **in the
same commit**. Agents read the Skill instead of `--help`, so a capability it
omits effectively does not exist for them, and no Skill claim may contradict the
code. Bump the Skill `version` when its content changes; the handshake compares
it against the embedded copy.

Executable shell examples in the Skill's references are tested
(`skill_examples_test.go`); run them against failing stubs when editing them.

## Commits and pull requests

- Keep commits scoped to one logical change.
- Write imperative commit subjects (`fix: …`, `feat: …`, `docs: …`).
- PRs should describe the change and note how it was verified (`make test`,
  `make e2e`, a live check).
