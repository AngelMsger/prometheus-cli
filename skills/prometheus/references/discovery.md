# Discovery: metrics, labels and series

An empty PromQL result is not an error. This is the routine for finding out
which kind of "empty" you are looking at.

## The three levels

```bash
# 1. what metrics exist (name + type + help text)
prometheus-cli metadata list --metric http_requests_total
prometheus-cli labels values __name__ --since 1h        # every metric name

# 2. what labels those metrics carry
prometheus-cli labels list --match 'http_requests_total' --since 1h
prometheus-cli labels values job --match 'http_requests_total' --since 1h

# 3. what a full selector actually matches
prometheus-cli series list --match 'http_requests_total{job="api"}' --since 1h
```

Metric names live in the reserved `__name__` label, which is why
`labels values __name__` is the metric list.

## Why the window matters

`--since` / `--from` / `--to` are **optional** on these three commands. Without
one the server scans its entire retention, which is slow on a large instance and
will surface long-deleted series. With one you get "what exists now", which is
almost always the question. Use a window that covers the incident you are
investigating.

`series list`, `labels list` and `labels values` apply a default `--limit` so a
stray call cannot flood the context. Pass `--limit 0` to lift it when you
genuinely need the full set — and expect it to be large.

## Diagnosing an empty result

Work down the list; stop at the first one that explains it.

1. **Wrong metric name.** `metadata list --metric <name>` returns nothing →
   the metric is not known to this server. Search with
   `labels values __name__ --since 1h`.
2. **Wrong label value.** `labels values <label> --match '<metric>' --since 1h`
   shows what the label actually takes. Values are case-sensitive.
3. **Selector too narrow.** `series list --match '<selector>'` returns nothing
   while a looser selector returns series → one of the matchers is wrong.
4. **Wrong window.** The series exists but has no samples in your window —
   widen `--since`, or check when it was last scraped with `target list`.
5. **Not being scraped.** `target list --unhealthy` (the scrape is failing) and
   `target list --state dropped` (relabelling dropped the target before it was
   ever scraped). This is the cause whenever a metric disappeared rather than
   never existing.
6. **Wrong tenant.** On Cortex/Mimir/Thanos, a missing or wrong `--tenant`
   returns an empty result rather than an error. `config show` reports the
   resolved tenant.

## Metric type disagreements

When the same metric name is exported by more than one job, the aggregated
`metadata list` shows only one definition. `metadata targets --metric <name>`
shows it per target, with each target's labels — which is how you find an
exporter disagreeing with the rest about the type or the help text.

## Cardinality

`status tsdb` reports the head block's biggest contributors: series count by
metric name, by label pair, and the labels consuming the most memory. That is
the direct answer to "which metric is making this server expensive", and the
place to look before adding another label to something.
