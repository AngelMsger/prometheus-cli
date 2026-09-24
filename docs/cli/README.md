# prometheus-cli command reference

This index is generated from the CLI command tree — do not edit it by
hand; run `make docs`. The full reference, with every flag and example,
is published at <https://angelmsger.github.io/prometheus-cli/cli/>.

## admin

| Command | Description |
| --- | --- |
| [`prometheus-cli admin`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-admin) | TSDB administration: delete series, clean tombstones, snapshot (writes) |
| [`prometheus-cli admin clean-tombstones`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-admin-clean-tombstones) | Erase already-deleted blocks from disk (destructive write) |
| [`prometheus-cli admin delete-series`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-admin-delete-series) | Mark the samples matching a selector as deleted (destructive write) |
| [`prometheus-cli admin snapshot`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-admin-snapshot) | Write a TSDB snapshot (write) |

## alert

| Command | Description |
| --- | --- |
| [`prometheus-cli alert`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-alert) | Inspect active alerts and the Alertmanagers they go to |
| [`prometheus-cli alert list`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-alert-list) | List the currently active alerts |
| [`prometheus-cli alert managers`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-alert-managers) | List the Alertmanager endpoints Prometheus is sending to |

## auth

| Command | Description |
| --- | --- |
| [`prometheus-cli auth`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-auth) | Log in, check reachability and log out |
| [`prometheus-cli auth guide`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-auth-guide) | Show offline credential acquisition guidance for this service |
| [`prometheus-cli auth login`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-auth-login) | Store credentials for the active context (interactive) |
| [`prometheus-cli auth logout`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-auth-logout) | Remove the stored credential for the active context |
| [`prometheus-cli auth reuse`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-auth-reuse) | Reuse an existing login in the selected context without signing in again |
| [`prometheus-cli auth status`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-auth-status) | Show what this context sends and whether the server answers |

## config

| Command | Description |
| --- | --- |
| [`prometheus-cli config`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-config) | Set up and inspect configuration and contexts |
| [`prometheus-cli config contexts`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-config-contexts) | List configured contexts and which one is current |
| [`prometheus-cli config init`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-config-init) | Interactively configure a context and store credentials |
| [`prometheus-cli config set-context`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-config-set-context) | Configure service presets without credentials or network access |
| [`prometheus-cli config show`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-config-show) | Show the resolved configuration with field provenance |
| [`prometheus-cli config use-context`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-config-use-context) | Set the current context |

## doctor

| Command | Description |
| --- | --- |
| [`prometheus-cli doctor`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-doctor) | Check configuration, credentials and connectivity |

## labels

| Command | Description |
| --- | --- |
| [`prometheus-cli labels`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-labels) | Discover label names and their values |
| [`prometheus-cli labels list`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-labels-list) | List label names |
| [`prometheus-cli labels values`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-labels-values) | List the values a label takes |

## metadata

| Command | Description |
| --- | --- |
| [`prometheus-cli metadata`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-metadata) | Look up what a metric is: its type, help text and unit |
| [`prometheus-cli metadata list`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-metadata-list) | List metric metadata (type, help, unit) |
| [`prometheus-cli metadata targets`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-metadata-targets) | Show metric metadata as individual scrape targets report it |

## query

| Command | Description |
| --- | --- |
| [`prometheus-cli query`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-query) | Run PromQL queries (instant, range and exemplars) |
| [`prometheus-cli query exemplars`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-query-exemplars) | List the trace exemplars attached to a selector |
| [`prometheus-cli query format`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-query-format) | Pretty-print a PromQL expression (and confirm it parses) |
| [`prometheus-cli query instant`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-query-instant) | Evaluate a PromQL expression at a single instant |
| [`prometheus-cli query parse`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-query-parse) | Show the server's parse tree for a PromQL expression |
| [`prometheus-cli query range`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-query-range) | Evaluate a PromQL expression across a time window |

## rule

| Command | Description |
| --- | --- |
| [`prometheus-cli rule`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-rule) | Inspect recording and alerting rules |
| [`prometheus-cli rule list`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-rule-list) | List rules, with their health and active alerts |

## series

| Command | Description |
| --- | --- |
| [`prometheus-cli series`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-series) | Discover the series a selector matches |
| [`prometheus-cli series list`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-series-list) | List the label sets matching one or more selectors |

## skill

| Command | Description |
| --- | --- |
| [`prometheus-cli skill`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-skill) | Install the companion Skill for coding agents |
| [`prometheus-cli skill install`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-skill-install) | Deploy the embedded Skill into a coding agent's skills directory |
| [`prometheus-cli skill path`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-skill-path) | Print Skill paths, installation state and version alignment |
| [`prometheus-cli skill show`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-skill-show) | Print the embedded SKILL.md to stdout |
| [`prometheus-cli skill status`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-skill-status) | Report loaded, installed and embedded Skill versions |
| [`prometheus-cli skill uninstall`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-skill-uninstall) | Remove the companion Skill from a coding agent's skills directory |

## status

| Command | Description |
| --- | --- |
| [`prometheus-cli status`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status) | Read the server's configuration, flags and storage stats |
| [`prometheus-cli status buildinfo`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-buildinfo) | Show the server's version and build metadata |
| [`prometheus-cli status config`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-config) | Show the server's running configuration (YAML) |
| [`prometheus-cli status flags`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-flags) | Show the command-line flags the server was started with |
| [`prometheus-cli status notifications`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-notifications) | Show the server's active notifications |
| [`prometheus-cli status runtimeinfo`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-runtimeinfo) | Show runtime state: uptime, reload time, goroutines, storage retention |
| [`prometheus-cli status tsdb`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-tsdb) | Show storage cardinality: the biggest metrics, labels and label values |
| [`prometheus-cli status wal-replay`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-status-wal-replay) | Show write-ahead-log replay progress |

## target

| Command | Description |
| --- | --- |
| [`prometheus-cli target`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-target) | Inspect scrape targets and why they are failing |
| [`prometheus-cli target list`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-target-list) | List scrape targets, active and dropped |

## version

| Command | Description |
| --- | --- |
| [`prometheus-cli version`](https://angelmsger.github.io/prometheus-cli/cli/#prometheus-cli-version) | Print version information |
