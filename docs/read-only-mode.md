# Read-only mode

`prometheus-cli` is a read tool. Querying, discovery and every `status` topic
only read. Exactly three commands change anything, and they all live under
`admin`:

| Command | Effect |
|---|---|
| `admin delete-series` | marks the samples matching a selector as deleted |
| `admin clean-tombstones` | erases already-deleted blocks from disk, irreversibly |
| `admin snapshot` | writes a TSDB snapshot under the server's data directory |

Read-only mode blocks those three **before a request leaves the process**.

## Turning it on

```bash
export PROMETHEUS_CLI_READ_ONLY=1
```

or, persistently, in `~/.angelmsger/prometheus/config.yaml`:

```yaml
defaults:
  read_only: true
```

A blocked write fails with a structured error and exit code 5 (`permission`):

```json
{
  "error": {
    "category": "permission",
    "code": "READONLY_BLOCKED",
    "message": "refusing to delete series: read-only mode is enabled",
    "next_steps": [
      "Add --dry-run to preview the request without sending it",
      "Add --allow-writes to override read-only mode for this command",
      "unset PROMETHEUS_CLI_READ_ONLY / set defaults.read_only=false to disable it"
    ]
  }
}
```

## The escape hatch

`--allow-writes` lifts the block for a single invocation. It is per-call on
purpose: a session asserted as read-only should not be silently turned writable
for everything that follows.

## Four layers, not one

Read-only mode is the outermost of four independent gates. A write has to pass
all of them:

1. **Read-only mode** — `PROMETHEUS_CLI_READ_ONLY` / `defaults.read_only`,
   overridable per call with `--allow-writes`.
2. **`--yes`** — required by the two destructive commands
   (`delete-series`, `clean-tombstones`). It **never prompts**: an agent or CI
   run has no terminal to answer on, so a missing `--yes` is a structured usage
   error (exit 2), not a hang. `snapshot` adds data rather than destroying it
   and needs no `--yes`.
3. **A required selector** — `delete-series` refuses to run without `--match`,
   because a delete with no selector would match every series.
4. **The server** — the admin endpoints exist only when Prometheus was started
   with `--web.enable-admin-api`. Without it the CLI reports
   `ADMIN_API_DISABLED` (exit 5) rather than a bare 404. Check with
   `prometheus-cli status flags`.

## `--dry-run` is a preview, not an approval

Every write accepts `--dry-run`, which prints the exact request that would be
sent plus a plain-language `effect`, and sends nothing:

```json
{
  "dry_run": true,
  "method": "POST",
  "url": "http://localhost:9090/api/v1/admin/tsdb/delete_series",
  "body": "match%5B%5D=up%7Bjob%3D%22retired%22%7D",
  "effect": "mark every sample matching up{job=\"retired\"} as deleted in the selected window; the data is removed on the next compaction or clean-tombstones"
}
```

Two things to keep in mind:

- It works **even in read-only mode**, because it never reaches the client
  method the read-only wrapper guards.
- It shows the **request**, not the series the request will hit. Confirm those
  separately with `series list --match '<selector>' --since <window>`.

## How it is enforced

`apiclient.NewReadOnly` wraps the client and overrides the three mutating
methods to return `READONLY_BLOCKED`. Reads pass straight through. Because the
gate lives in the client rather than in each command, a new write command
cannot forget it: it gets one override in
[`pkg/apiclient/readonly.go`](../pkg/apiclient/readonly.go) and nothing else
changes.

The HTTP transport also never retries a non-idempotent request, so a write is
sent at most once even when the server is returning 503s.
