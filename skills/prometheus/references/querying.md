# Querying: instant, range, and getting the step right

## Instant vs range

| Question | Command | Result shape |
|---|---|---|
| What is it **now** | `query instant` | one `value` per series (`resultType: vector`) |
| How did it **behave** | `query range` | a list of `values` per series (`resultType: matrix`) |

`query instant` is also the right tool for a single number you intend to quote —
`sum(rate(...))`, a percentile, a ratio. Do not run a range query and read its
last point; that point is at the step boundary, not at now.

## Samples are normalized

Prometheus sends every sample as a positional tuple, `[1758326400.5, "1"]`. The
CLI returns named fields instead:

```json
{"timestamp": 1758326400.5, "time": "2025-09-20T00:00:00.5Z", "value": "1"}
```

`value` stays a **string** on purpose: `NaN`, `+Inf` and full float precision
cannot survive a JSON number. Quote the `time`, not the `timestamp`.

Native histogram samples come back with a `histogram` object in place of
`value`, passed through verbatim.

## The step

`--step` is optional on `query range`. Prometheus refuses any range query that
would resolve to more than **11,000 points per series**, so:

- with no `--step`, the CLI derives a round resolution (~250 points for the
  window: `15s` for an hour, `10m` for a day, `1h` for a week);
- with a `--step` that would overrun the ceiling, the CLI raises it instead of
  sending a request that is guaranteed to fail.

Either way the result says what happened:

```json
"step": { "value": "10m", "source": "derived", "points": 145 }
```

`source` is `flag`, `derived` or `clamped`. **Quote the step whenever you quote
the numbers** — a rate averaged over `1h` steps and one over `15s` steps are
different claims.

Pass an explicit `--step` when the question depends on resolution (catching a
short spike) or when comparing against a dashboard that uses a known step.

## Range selectors vs the query window

These are two different things and mixing them up is the most common PromQL
error:

- `[5m]` inside the expression is the **range selector** — how much data each
  evaluation looks back over. `rate(x[5m])` needs at least a few scrapes in
  those 5 minutes.
- `--since 1h` is the **query window** — the span the expression is evaluated
  across.

A rule of thumb: the range selector should be at least 4× the scrape interval
(`status config` shows `scrape_interval`), and no larger than necessary.

## Counters, gauges and histograms

`metadata list --metric <name>` gives the type. It changes the query:

- **counter** (`http_requests_total`) — monotonic, resets on restart. Always
  `rate(x[5m])` or `increase(x[1h])`; a raw value or a bare `sum()` is
  meaningless.
- **gauge** (`up`, memory usage) — read directly; aggregate with
  `avg`/`max`/`min`.
- **histogram** (`..._bucket`) — percentiles come from
  `histogram_quantile(0.99, sum by (le) (rate(x_bucket[5m])))`. The `le` label
  must survive the aggregation.
- **summary** (`..._quantile`) — quantiles are precomputed per instance and
  **cannot be aggregated**; read them per series.

## Long or awkward expressions

Shell-quoting a real expression is error-prone. Both forms avoid it:

```bash
prometheus-cli query range --query @slo.promql --since 24h
echo 'sum by (job) (rate(http_requests_total[5m]))' | prometheus-cli query instant --query @-
```

## Checking an expression without running it

```bash
prometheus-cli query format --query '<promql>'   # canonical form; confirms it parses
prometheus-cli query parse  --query '<promql>'   # the server's parse tree
```

`query parse` is what to reach for when a query runs but returns something
unexpected — the tree shows exactly how operators bound and what the matchers
resolved to.

## Server-side limits

- `--limit N` caps the number of returned series (server-side).
- `--query-timeout 10s` bounds evaluation on the server. It is distinct from the
  global `--timeout`, which bounds the HTTP request from this side.
- A `422` (`QUERY_EXECUTION`) means the expression parsed but could not run —
  usually too many samples. Narrow the selector, aggregate, shorten the window,
  or raise the step.

## Warnings

A result may carry `warnings` (for example a partial response from a federated
or sharded backend). They are part of the answer: a truncated result read as
complete is a wrong answer. In `--format ndjson` they are re-emitted on stderr
as a `query_advisories` notice.
