# Targets, rules, alerts and server state

## Targets — why a metric is missing

```bash
prometheus-cli target list --unhealthy            # active targets that are not up
prometheus-cli target list --state dropped        # never scraped: dropped by relabelling
prometheus-cli target list --scrape-pool node     # one pool
```

Active and dropped targets come back in **one list** with an explicit `state`,
each carrying `health`, `last_error`, `last_scrape` and `last_scrape_ago`.

- `--unhealthy` keeps only **active** targets whose health is not `up`. A
  dropped target is not unhealthy — it was never scraped.
- A dropped target has no post-relabelling labels, so its identity is reported
  from `discovered_labels`. That is also the only place the reason it was
  dropped is visible.
- `last_scrape_ago` is the staleness signal: a target reporting `up` but last
  scraped an hour ago is as broken as one reporting `down`.

## Rules

Rules are flattened out of their groups, one row per rule, each carrying
`group`, `file` and `group_interval_seconds`.

```bash
prometheus-cli rule list --type alert                    # alerting rules + their active alerts
prometheus-cli rule list --failing                       # last evaluation errored
prometheus-cli rule list --group node-alerts --exclude-alerts
prometheus-cli rule list --group-limit 20                # page by group; resume with --cursor
```

`--failing` matters because a broken rule is **silent**: Prometheus keeps
serving the rule's last successful result, so a recording rule that has been
erroring for a day looks like a metric that simply stopped moving. Check it
whenever a derived series looks stale.

On an instance with many firing alerts, the per-rule `alerts` array is most of
the response — `--exclude-alerts` drops it.

## Alerts

```bash
prometheus-cli alert list --state firing
prometheus-cli alert list --name HighRequestLatency
prometheus-cli alert managers
```

`alert list` is Prometheus' own view: alerts **before** Alertmanager applies
grouping, inhibition and silencing. Do not report it as "what was paged".

Each alert carries `active_for` alongside `active_at`, and the `value` that
triggered it.

`alert managers` lists the Alertmanager endpoints, active and dropped. An empty
active list means notifications are going nowhere no matter how loudly the rules
fire — check it whenever an alert fired and nobody heard about it.

## Server state

```bash
prometheus-cli status config        # the loaded configuration (YAML)
prometheus-cli status flags         # retention, storage path, --web.enable-admin-api
prometheus-cli status runtimeinfo   # uptime, last successful reload, series count
prometheus-cli status buildinfo     # version — decides which endpoints exist
prometheus-cli status tsdb          # cardinality: what is making this server big
prometheus-cli status wal-replay    # a replaying server answers with incomplete data
prometheus-cli status notifications # the server's own banners (newer versions)
```

Two of these explain surprising results rather than describing the server:

- **`status config` is the loaded state, not the file on disk.** They differ
  whenever a change has not been reloaded — cross-check the last reload time in
  `status runtimeinfo`.
- **`status wal-replay`** explains an empty or partial result right after a
  restart.

## TSDB admin (writes)

These are the only commands that change anything, and they exist only when the
server was started with `--web.enable-admin-api` (confirm with `status flags`).
Without it they fail with `ADMIN_API_DISABLED` (exit 5).

```bash
# 1. confirm exactly what the selector matches
prometheus-cli series list --match 'up{job="retired"}' --since 30d
# 2. preview the request
prometheus-cli admin delete-series --match 'up{job="retired"}' --dry-run
# 3. execute
prometheus-cli admin delete-series --match 'up{job="retired"}' --yes
```

- `delete-series` requires a `--match` selector: without one it would match
  every series. It marks samples deleted; they stop being queryable immediately
  and leave disk on the next compaction.
- `clean-tombstones` erases already-deleted blocks **immediately and
  irreversibly**. It takes no selector — it applies to everything marked deleted
  on the server.
- `snapshot` adds data rather than destroying it, so it needs no `--yes`, but it
  consumes disk on the server.

Both destructive commands require `--yes` and **never prompt**: an agent or CI
run has no terminal to answer on, so a missing `--yes` is a structured usage
error (exit 2), not a hang. `--dry-run` works even in read-only mode and prints
the exact request plus an `effect` sentence; it is a preview, not an approval.
