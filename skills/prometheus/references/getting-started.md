# Getting started: configuration and auth

`prometheus-cli` talks to any Prometheus-compatible server (Prometheus, Thanos
Query, Cortex/Mimir, VictoriaMetrics), default `http://localhost:9090`.

**`--base-url` is the server root** — the part *before* `/api/v1`. A pasted API
endpoint is trimmed back automatically, but a Grafana URL is not a Prometheus
API and will fail with `NOT_PROMETHEUS_API`.

## Authentication

Prometheus ships with **no user accounts**. The three schemes are:

| Scheme | When | Secret |
|---|---|---|
| `none` (default) | a Prometheus reachable on its own port | none |
| `bearer` | hosted vendors, Kubernetes service accounts, gateways | a token |
| `basic` | a reverse proxy / ingress / `web.yml` `basic_auth_users` | username + password |

Multi-tenant backends (Cortex, Mimir, Thanos Receive) additionally need
`--tenant <id>`, sent as `X-Scope-OrgID`. **A missing tenant returns an empty
result, not an error** — a silent wrong answer, so set it when the backend is
multi-tenant.

`auth guide` prints where the credential actually comes from for the configured
scheme. It never invents a token URL, because Prometheus has no credential page.

## Interactive setup (humans, on a terminal)

```
prometheus-cli config init --pretty   # interactive TUI (recommended for humans)
prometheus-cli config init             # plain line-by-line wizard (works over a pipe)
```

For the default `none` scheme it asks only for the URL. It writes a context to
`~/.angelmsger/prometheus/config.yaml`; any secret goes to the OS keychain
(falling back to per-user DPAPI on Windows or a `0600` file on macOS/Linux) and
never into the config file. When a config already exists, re-running it lists
the contexts and asks whether to **edit** one (prefilled), **add** a new one, or
**replace** everything; `config init --context prod` skips that prompt.

`--pretty` renders an interactive TUI and requires a terminal (otherwise it
fails with `PRETTY_NEEDS_TTY`); the plain wizard reads stdin line by line, so it
also works non-interactively.

`auth login` is the lighter form: it re-prompts only for the credential of the
already-configured context. On a `none` context it returns `AUTH_NOT_REQUIRED`
rather than storing an empty secret.

## For agents and sandboxes

The user has normally configured Prometheus already. Reuse their host config
under `~/.angelmsger/prometheus/` and OS keychain. If credential resolution
returns `CREDENTIAL_STORE_INACCESSIBLE` or `CREDENTIAL_NOT_VISIBLE_OR_MISSING`
with `recovery.scope=host`, request host access and retry the same invocation
once. Do not run `config init` or `auth login` inside the sandbox.

Only when the host retry also reports missing credentials should the user
configure them in their terminal or provide environment variables. The CLI
cannot and must not elevate itself; `recovery.scope=host` is an instruction to
the Agent host or approval layer.

## Headless setup (CI and genuinely unconfigured environments)

Environment variables take precedence over the config file and keychain:

```
export PROMETHEUS_URL=https://prometheus.example.com
# only if something in front of it authenticates:
export PROMETHEUS_TOKEN='your-bearer-token'
#   …or basic auth instead:
export PROMETHEUS_USER=alice
export PROMETHEUS_PASSWORD='your-password'
# multi-tenant backends only:
export PROMETHEUS_TENANT=team-a
```

`PROMETHEUS_TOKEN` selects the bearer scheme, `PROMETHEUS_PASSWORD` the basic
scheme; with neither set the scheme stays `none`.

## Verify

```
prometheus-cli auth status   # what this context sends + is the server answering
prometheus-cli doctor        # config / credentials / connectivity checks
```

There is no "who am I": Prometheus exposes no identity endpoint, so `auth
status` reports the resolved settings plus the server's version from build info.

## Precedence

Highest wins: command-line flags → environment variables → `.env` file → config
file → built-in defaults. `config show` prints the resolved values and where the
key ones came from.

## Read-only posture

Set `PROMETHEUS_CLI_READ_ONLY=1` (or `defaults.read_only: true` in the config
file) to assert a read-only session: the TSDB admin writes (`admin
delete-series`, `admin clean-tombstones`, `admin snapshot`) are then blocked
before any request is sent. `--allow-writes` is the per-call escape hatch, and
`--dry-run` previews a write without sending it even in read-only mode.

For preset team services, use `config set-context` and `auth guide` before
personal login; see [team setup](team-setup.md).
