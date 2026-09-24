# Technical design

`prometheus-cli` is a Go + Cobra CLI that mirrors the architecture of its
siblings (`openobserve-cli`, `jenkins-cli`, `confluence-cli`, `bitbucket-cli`,
`jira-cli`). This document covers the layout, the contracts that hold the layers
together, and the places where the Prometheus domain made us diverge.

## Layout

```
cmd/prometheus-cli    entry point (delegates to internal/app)
cmd/gen-docs          generates docs/cli/ from the cobra tree
internal/app          one file per noun (query, series, metadata, target, rule,
                      alert, status, admin, auth, config, doctor, skill) plus
                      root wiring and the shared appState
pkg/apiclient         the Prometheus HTTP API surface and normalized models
pkg/transport         retrying HTTP client with request decorators
pkg/errors            the CLIError model and exit-code map (0–11)
pkg/timeutil          human time expressions ⇄ Prometheus instants; step derivation
pkg/constants         app name, defaults, build-time version vars
internal/output       JSON / table / ndjson rendering, {items,next,has_more}
internal/config       layered configuration + named contexts
internal/auth         credential model + OS keychain storage
internal/cliflags     argv normalization (LLM slip correction)
internal/update       the passive "a newer release exists" check
skills/prometheus     the companion Skill, embedded into the binary
test/mockserver       a stand-in Prometheus API for the e2e suite
```

`internal/app/root.go` assembles the command tree; `internal/app/context.go`
holds `appState`, the per-invocation handle that every command captures.

## Request path

```
argv ──► cliflags.Normalize ──► cobra ──► appState.load (layered config)
                                             │
                                             ▼
                                   auth.Resolve (env/flags → keychain)
                                             │
                                             ▼
                          apiclient.BuildClient (URL + decorators + retry)
                                             │
                     read-only? ──► apiclient.NewReadOnly wrapper
                                             │
                                             ▼
                              Client method ──► transport.Client.Do
                                             │
                                             ▼
                          envelope decode ──► normalized model ──► output.Emit
```

Everything user-facing is a `*errors.CLIError` whose `Category` maps
deterministically to an exit code. Nothing prints to stdout except command data.

## The API surface

Prometheus returns one envelope from every `/api/v1` endpoint:

```json
{"status":"success","data":…,"warnings":[…]}
{"status":"error","errorType":"bad_data","error":"…"}
```

`apiclient.do` decodes it once and turns three cases into structured errors:

- **A non-2xx, or `status: "error"`.** The envelope's `errorType` refines the
  classification: `bad_data` and `execution` become **usage** errors, because
  the caller's PromQL is at fault and retrying is pointless; `timeout`,
  `canceled` and `unavailable` become **server** errors, which are retryable.
- **A 404 on an `/admin/` path.** Translated to `ADMIN_API_DISABLED`
  (permission), because that is what it actually means — the server was started
  without `--web.enable-admin-api` — rather than "no such resource".
- **A 200 without a `status` field.** `NOT_PROMETHEUS_API` (parse). This matters
  more than it looks: a `--base-url` pointing at a dashboard, a login page or a
  catch-all proxy route otherwise decodes to an empty payload, and an empty
  result is a plausible answer to almost any query. Failing loudly turns a
  misconfiguration into a diagnosis instead of a confident wrong answer.

A 204 with no body (the admin endpoints' success) is success, not a decode
failure.

## Normalized models

The client does not pass raw Prometheus JSON through. Three shapes are absorbed
here so no caller has to:

- **Sample tuples.** `[1758326400.5, "1"]` becomes
  `{timestamp, time, value}` — the raw fractional epoch plus its RFC3339
  rendering. `value` stays a **string**, because `NaN` and `+Inf` are not JSON
  numbers and re-encoding a float loses digits the server sent. Native
  histogram samples keep their body verbatim under `histogram`.
- **Two-array results.** Targets (`activeTargets`/`droppedTargets`) and
  Alertmanagers (`active`/`dropped`) become one list with an explicit `state`,
  so "what is broken" is one listing and one filter. A dropped target's
  identity is recovered from `discoveredLabels`, the only place it exists.
- **Nested groups.** Rules are flattened out of their groups into one row each,
  carrying `group`, `file` and `group_interval_seconds` — the identifiers the
  `--group` and `--file` filters take. Group paging is preserved through the
  server's own `groupNextToken`, surfaced as the family's `next` / `--cursor`.

`Series` carries either `value` (vector/scalar/string) or `values` (matrix);
`result_type` tells the caller which to read.

## Queries go out as POST

Every query endpoint is called with an `application/x-www-form-urlencoded` body
rather than a query string. Real PromQL expressions routinely exceed what
proxies accept in a URL, and Prometheus supports POST on all of them for exactly
that reason. `--query @file` and `--query @-` exist for the same problem one
layer up: shell-quoting an expression full of braces, quotes and backslashes is
where agents and humans both slip.

## Step derivation — a deliberate divergence

The sibling `openobserve-cli` requires `--step` on a range query. This CLI makes
it optional, because Prometheus has a hard ceiling its backend does not:
a range query resolving to more than **11,000 points per series** is rejected
outright.

That is an error class a caller can only discover by hitting it, and then has to
fix with arithmetic over a window they did not choose. So `pkg/timeutil.
ResolveStep` owns it:

- with no `--step`, it derives a round resolution near 250 points for the window
  (snapped to a ladder: `15s`, `1m`, `5m`, `1h`, …);
- with a `--step` that would overrun the ceiling, it raises the step rather than
  sending a request guaranteed to fail.

The result always reports what happened —
`{"value": "10m", "source": "derived"|"flag"|"clamped", "points": 145}` — so the
resolution behind the numbers is never implicit. The divergence and its reason
are recorded in the function's doc comment and covered by
`pkg/timeutil/step_test.go`.

## Authentication — `none` is a first-class scheme

Prometheus ships with no user database, so unlike every sibling the **default
scheme is `none`**: no credential is resolved, nothing is read from the
keychain, and `PROMETHEUS_URL` alone is a complete configuration.

`basic` and `bearer` cover the deployments that do authenticate (a reverse
proxy, an ingress, a hosted vendor, a Kubernetes service account). A `none`
credential is still a `Credential` and still produces a decorator, because it
carries the other piece of request identity:

`auth.Credential.TenantID` is sent as `X-Scope-OrgID` for Cortex, Mimir and
Thanos Receive. It is not a secret, and it is not personal — every member of a
team queries the same tenant — so it is a **service preset**, distributed with
the URL by `config set-context` rather than collected at login. This matters
because a multi-tenant backend answers an untenanted request with an *empty
result*, not an error: a missing tenant is a silent wrong answer.

`AccountKey` includes the URL path, so one gateway host fronting several
path-routed deployments does not collapse their secrets into one keychain entry.

Verification (`auth login`, `doctor`) reads `status/buildinfo`. Prometheus
exposes no identity endpoint, so there is no authenticated user to assert; a
successful build-info read proves the URL is a Prometheus API and that whatever
fronts it accepted the credential, which is the strongest check available.

## Base URL normalization

`NormalizeBaseURL` supplies a scheme for a bare `host:port`, trims a trailing
slash, and **trims a trailing `/api/v1`**. Pasting the API endpoint instead of
the server root is the most common Prometheus setup mistake, and storing it
would turn every later command into a 404. `config.NormalizeServiceURL` does the
same at the persistence boundary, so a context never records one.

## Discoverability — no dead-end inputs

Every identifier a command accepts is discoverable from another command:

| Input | Discovered by |
|---|---|
| metric name | `metadata list`, `labels values __name__` |
| label name | `labels list` |
| label value | `labels values <label>` |
| series selector | `series list` |
| scrape pool, target labels | `target list` |
| rule group, rule file, rule name | `rule list` |
| alert name | `rule list --type alert`, `alert list` |
| status topic | `status --help` (each topic is its own subcommand) |
| context name | `config contexts` |

Where a command cannot list its own inputs, its error `next_steps` names the one
that can.

## Output

`internal/output` renders JSON (default), a human table, or ndjson. Listings use
the family envelope `{items, next, has_more}`. A query result is a single
document in JSON; under `--format ndjson` it streams one series per line and
re-emits the server's `warnings` as a `query_advisories` notice on **stderr**,
so a truncation warning is not lost when only the series are piped onward.

`--fields a,b.c` projects each record to those dot-paths. Projection applies to
the emitted document, so for a query result use it together with
`--format ndjson`, where each line is a series.

## Testing

- `go test ./...` — unit tests, including the normalization, error
  classification, step derivation and credential contracts, plus
  `skill_examples_test.go`, which executes the shell example embedded in the
  Skill's error reference against stubs at every documented exit code.
- `./scripts/e2e.sh` — the built binary against `test/mockserver`, asserting the
  agent-facing contract end to end: output shapes, exit codes, write gating,
  the bearer + path-routed base URL path, and the Skill handshake.
- `./scripts/e2e-setup.sh` — team distribution with no network and no access to
  personal state.

### Existing credential reuse

`internal/app/auth_reuse.go` matches stored contexts and verifies credentials
through native auth/client code before associating identity with the destination.
It never writes the credential store, consumes environment secrets or changes
current_context. Public `config set-context` remains offline and credential-free.
The destination's complete service identity and the captured config are checked
again before writing. See the installation guide for the result and recovery contract.

Equivalent service URL overrides preserve the persisted native credential lookup
key without redirecting requests or copying secrets. Logout removes that same
entry. A different complete deployment URL cannot use the retained lookup key.
