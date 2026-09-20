# Errors and exit codes

Every failure is JSON on **stderr** (stdout stays clean), shaped:

```json
{
  "error": {
    "category": "usage",
    "code": "BAD_QUERY",
    "message": "Prometheus returned HTTP 400 (bad_data): parse error: unknown function \"rat\"",
    "hint": "Prometheus rejected the request — usually a PromQL syntax error, an unknown metric name, or a malformed time or duration.",
    "next_steps": [
      "prometheus-cli query parse --query 'rat(up[5m])'",
      "prometheus-cli metadata list --metric <name>",
      "prometheus-cli labels values __name__ --since 1h"
    ],
    "http_status": 400
  }
}
```

Read `next_steps` first — it names the command to run next. `retryable` (when
present) tells you whether a retry in the same environment could help
(rate-limit / network / server). Environment changes use the optional
`recovery` object instead.

## Category → exit code

| Category     | Exit | Meaning / typical fix |
|--------------|------|-----------------------|
| (success)    | 0    | — |
| `internal`   | 1    | Unexpected bug; retry with `--verbose`. |
| `usage`      | 2    | Bad flag, bad PromQL, missing window, missing `--yes`. |
| `config`     | 3    | Config/credential resolution failed; inspect `code` and `recovery` before reconfiguring. |
| `auth`       | 4    | A gateway rejected the credentials → `auth status`, `auth guide`. |
| `permission` | 5    | Not allowed, the admin API is disabled, or read-only mode blocked a write. |
| `not_found`  | 6    | No such endpoint → `doctor`, `status buildinfo`. |
| `rate_limit` | 7    | Too many requests; retryable. |
| `network`    | 8    | Server unreachable; retryable → `doctor`, check `--base-url`. |
| `server`     | 9    | Prometheus 5xx, or a query timeout/cancellation; retryable. |
| `parse`      | 10   | The response was not a Prometheus API reply; retry with `--verbose`. |
| `conflict`   | 11   | Resource changed since read; re-fetch then retry. |

Scripted use:

```bash
result_file=$(mktemp)
trap 'rm -f "$result_file"' EXIT
if prometheus-cli query instant --query 'up == 0' >"$result_file"; then
  cat "$result_file"
else
  code=$?
  case "$code" in
    2) printf '%s\n' 'Fix the query or flags; check the structured next_steps.' >&2 ;;
    3|4) printf '%s\n' 'Inspect the structured auth/config recovery steps.' >&2 ;;
    5) printf '%s\n' 'Not permitted, or a write was blocked by read-only mode.' >&2 ;;
    7|8|9) printf '%s\n' 'Transient read failure; retry within a bounded budget.' >&2 ;;
    *) printf 'Command failed with exit %s.\n' "$code" >&2 ;;
  esac
  exit "$code"
fi
```

## Common cases

- **An empty result is not an error (exit 0).** `{"series": []}` means nothing
  matched. See [discovery.md](discovery.md) before reporting it as healthy.
- **`BAD_QUERY` (usage/2)** — Prometheus rejected the expression: a syntax
  error, an unknown function, or a malformed duration. `query parse` shows how
  it was read; `metadata list` confirms the metric exists.
- **`QUERY_EXECUTION` (usage/2)** — the expression parsed but could not run,
  almost always too many samples. Narrow the selector, aggregate with
  `sum by (...)`, shorten the window, or raise `--step`.
- **`BAD_TIME_RANGE` (usage/2)** — `query range` needs `--since` or
  `--from`/`--to`, ordered and non-empty.
- **`BAD_STEP` (usage/2)** — `--step` must be a positive duration (`30s`, `5m`)
  or a bare number of seconds. Omit it to let the CLI derive one.
- **`CONFIRMATION_REQUIRED` (usage/2)** — a destructive admin write needs
  `--yes`. Preview with `--dry-run` first.
- **`AUTH_NOT_REQUIRED` (usage/2)** — `auth login` on a `none` context. A plain
  Prometheus stores no credential; run `doctor` instead.
- **`NO_BASE_URL` (config/3)** — no server configured. Run `config init` or set
  `PROMETHEUS_URL`.
- **`CREDENTIAL_STORE_INACCESSIBLE` / `CREDENTIAL_NOT_VISIBLE_OR_MISSING`
  (config/3)** — when `recovery.scope` is `host`, request host access and retry
  the same invocation once. Repeating it in the same sandbox will not help.
  Only configure credentials when the host retry also reports them missing.
- **`HTTP_UNAUTHORIZED` (auth/4)** — a proxy or hosted endpoint rejected the
  credential. Prometheus itself has no accounts, so this is always something in
  front of it. `auth guide` says where the credential comes from.
- **`ADMIN_API_DISABLED` (permission/5)** — the server was started without
  `--web.enable-admin-api`. Confirm with `status flags`; it cannot be enabled
  from here.
- **`READONLY_BLOCKED` (permission/5)** — a write was blocked because the
  session is read-only. Preview with `--dry-run`; use `--allow-writes` only for
  a write the user authorized.
- **`NOT_PROMETHEUS_API` (parse/10)** — a 200 response without the Prometheus
  envelope: `--base-url` points at something else (a dashboard, a login page, a
  catch-all proxy route). It must be the server root, not `/api/v1` and not a
  Grafana URL.
- **`UNKNOWN_STATUS_TOPIC` (usage/2)** — the topic list is in
  `status --help`; older servers lack `notifications`.
