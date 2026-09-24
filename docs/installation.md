# Installation and setup

## Install

### npm (recommended)

```bash
npm install -g @angelmsger/prometheus-cli
```

The postinstall script downloads the prebuilt binary for your platform from the
matching GitHub Release and verifies its SHA-256 against the release's
`checksums.txt`. Installing with `--ignore-scripts` still works: the launcher
fetches the binary on first run.

### go install

```bash
go install github.com/angelmsger/prometheus-cli/cmd/prometheus-cli@latest
```

### Prebuilt binary

Download the asset for your platform from the
[Releases page](https://github.com/AngelMsger/prometheus-cli/releases), verify
it against `checksums.txt`, then move it onto your `PATH`.

### From source

```bash
git clone https://github.com/AngelMsger/prometheus-cli.git
cd prometheus-cli
make build          # ./bin/prometheus-cli
make install        # copies it to $GOBIN (or $GOPATH/bin)
```

## Finish setup

### 1. Deploy the companion Skill

The `prometheus` Skill is embedded in the binary, so it always matches the
installed version. It detects the coding agents on the machine and installs into
each one's skills directory:

```bash
prometheus-cli skill install
prometheus-cli skill status      # loaded / installed / embedded versions
```

Re-run it after every CLI upgrade, then reload the agent context.

### 2. Shell completion

```bash
source <(prometheus-cli completion bash)     # bash
prometheus-cli completion zsh  > "${fpath[1]}/_prometheus-cli"
prometheus-cli completion fish > ~/.config/fish/completions/prometheus-cli.fish
prometheus-cli completion powershell | Out-String | Invoke-Expression
```

## Configure

### The base URL

`--base-url` / `PROMETHEUS_URL` is the **server root** — the part before
`/api/v1`. A pasted API endpoint is trimmed back automatically, so
`https://prom.example.com/api/v1` and `https://prom.example.com` are equivalent.
A Grafana URL is not a Prometheus API and fails with `NOT_PROMETHEUS_API`.

The default is `http://localhost:9090`.

### Authentication

Prometheus has no user accounts of its own, so the default scheme is `none` and
`PROMETHEUS_URL` alone is often a complete configuration.

| Scheme | Use it when | Secret |
|---|---|---|
| `none` (default) | Prometheus is reachable on its own port | — |
| `bearer` | a hosted vendor, a Kubernetes service account, a token gateway | a token |
| `basic` | a reverse proxy / ingress / `web.yml` `basic_auth_users` | username + password |

Multi-tenant backends (Cortex, Mimir, Thanos Receive) additionally need
`--tenant <id>` / `PROMETHEUS_TENANT`, sent as `X-Scope-OrgID`. A missing tenant
returns an **empty result rather than an error**, so set it whenever the backend
is multi-tenant.

`prometheus-cli auth guide` prints where the credential comes from for the
configured scheme.

### Interactive setup

```bash
prometheus-cli config init --pretty   # interactive TUI (needs a terminal)
prometheus-cli config init             # plain wizard; also works over a pipe
```

For the default `none` scheme the wizard asks only for the URL and the optional
tenant. It writes a context to `~/.angelmsger/prometheus/config.yaml`; a secret,
if the scheme has one, goes to the OS keychain — never to the config file.

When a configuration already exists, re-running `config init` lists the contexts
and asks whether to **edit** one (prefilled), **add** a new one, or **replace**
everything. `config init --context prod` skips that question and targets the
named context directly.

`auth login` is the lighter form: it re-prompts only for the credential of the
already-configured context. On a `none` context it returns `AUTH_NOT_REQUIRED`
rather than storing an empty secret.

### Headless setup

Environment variables take precedence over the config file and the keychain:

```bash
export PROMETHEUS_URL=https://prometheus.example.com
export PROMETHEUS_TOKEN='your-bearer-token'     # selects the bearer scheme
# …or basic auth instead:
export PROMETHEUS_USER=alice
export PROMETHEUS_PASSWORD='your-password'      # selects the basic scheme
export PROMETHEUS_TENANT=team-a                 # multi-tenant backends only
export PROMETHEUS_FORMAT=json
export PROMETHEUS_CLI_READ_ONLY=1               # block the TSDB admin writes
```

With neither secret set the scheme stays `none`. See
[`.env.example`](../.env.example) for the full list.

### Precedence

Highest wins: **command-line flags → environment variables → `.env` file →
config file → built-in defaults.**

`prometheus-cli config show` prints the resolved values and where the key ones
came from.

### Multiple servers

Contexts are kubectl-style and live in one config file:

```bash
prometheus-cli config contexts             # list them, and which is current
prometheus-cli config use-context prod     # change the default
prometheus-cli --use-context staging query instant --query up
```

When more than one context is configured and none was selected explicitly, the
CLI emits a one-line notice on stderr naming the context it fell back to —
silence it with `PROMETHEUS_CLI_NO_CONTEXT_HINT=1`.

## Verify

```bash
prometheus-cli auth status   # what this context sends + is the server answering
prometheus-cli doctor        # config / credentials / connectivity / Skill
```

There is no "who am I": Prometheus exposes no identity endpoint, so `auth
status` reports the resolved settings plus the server's version from build info.

## Team distribution and personal login

Distribute the **service** settings separately from each member's credentials.
`config set-context` writes a named context offline: it never touches the
network, never reads or writes the keychain, and ignores personal identity and
secrets found in the environment.

```bash
# an internal Prometheus with no authentication — this is the whole setup
prometheus-cli config set-context team \
  --base-url https://prometheus.example.com --auth-scheme none --activate

# a multi-tenant backend behind a gateway
prometheus-cli config set-context mimir \
  --base-url https://mimir.example.com \
  --auth-scheme bearer --tenant team-a \
  --credential-url https://help.example.com/prometheus-tokens
```

The tenant id is a service preset, not an identity: everyone queries the same
`X-Scope-OrgID`, so it travels with the URL and scheme.

Preview any of it with `--dry-run`, which runs the same merge and conflict
checks and writes nothing.

**Conflicts are explicit.** A preset that would change an existing non-empty
service field fails with `CONFIG_CONTEXT_CONFLICT` and a `details` object
carrying each field's `before` and `after`. Inspect the difference, then either
re-run with `--overwrite` or choose another context name. Identical presets do
not rewrite the file. Unspecified fields, other contexts, shared defaults and
stored usernames are always preserved. The first context becomes current;
later calls change the current context only with `--activate`.

Then each member finishes in their own terminal — and only if the context has a
scheme that needs a credential:

```bash
prometheus-cli --use-context mimir auth guide
prometheus-cli --use-context mimir auth login
```

`auth guide` is offline and display-only; no request is ever sent to the
credential URL. It reports `server`, `scheme`, `credential_url`, `source`
(`flag`, `env`, `dotenv`, `file`, `builtin` or `fallback`), `instructions`,
`documentation_url` and `next_steps`. Because Prometheus has no credential page
of its own, with nothing configured it falls back to the server root and says
so rather than naming a token URL that does not exist — set `--credential-url`
to your team's own page.

`auth login` verifies the complete normalized service URL **before** storing
anything. A mismatch returns `CONTEXT_BASE_URL_MISMATCH` and stores nothing;
select or create a matching context. If the secret is stored but the identity
cannot be written back, the error is `LOGIN_CONFIG_WRITE_FAILED` with
`credential_stored: true` plus the server, context and scheme — fix the config
directory's permissions and repeat the login.

Never respond to an inaccessible host credential
(`CREDENTIAL_STORE_INACCESSIBLE`, `CREDENTIAL_NOT_VISIBLE_OR_MISSING`) by
running fresh setup inside a sandbox. Those errors carry
`recovery.scope: "host"`: retry the same command with access to the host
environment first, and reconfigure only if that also reports the credential
missing.

Do not collect secrets through chat or pass them as command arguments. In CI,
use transient environment variables.

## Credential storage

Secrets go to the OS keychain (Keychain on macOS, Secret Service on Linux,
Credential Manager on Windows). When no keychain is available the CLI falls back
to a file under the config directory: `0600` on macOS/Linux, and encrypted with
per-user DPAPI on Windows, where `FileMode` carries no POSIX meaning.

The keychain account key includes the URL path, so one gateway host fronting
several path-routed deployments keeps their credentials separate.

## Read-only mode

See [read-only-mode.md](read-only-mode.md) for the write-safety posture around
the three TSDB admin commands.

## Reuse an existing login

After preparing a team context, associate an existing personal login without
creating another token or changing the active context:

```sh
prometheus-cli --use-context team auth reuse --dry-run
prometheus-cli --use-context team auth reuse
prometheus-cli --use-context team auth status
```

Reuse matches the complete normalized service URL, authentication scheme and
provider scope (deployment flavor, organization or tenant where applicable).
It verifies the source credential before filling missing destination identity.
A populated destination identity is preserved. Credentials stay in their native
store; no secrets or environment credentials are copied. Both source and unrelated
contexts remain unchanged. Configuration changes during verification stop the write.

The JSON result reports `context`, `state`, `changed`, `verified`, `dry_run` and,
when selected, `source_context`. States are `available` (verified preview),
`reused`, `unchanged`, or `unavailable`. The last two do not establish successful
authentication: always retain the separate `auth status` check. No reusable
identity is a normal no-change result. Network, permission and credential-store
failures retain their structured errors instead of suggesting a fresh login.

Multiple different verified identities return `AUTH_REUSE_AMBIGUOUS`; discover
context names with `config contexts`, then repeat with `--from-context <name>`.
The command does not replace identities or switch authentication schemes. As
with native `auth login`, updating this CLI's own settings remains available
in remote read-only mode; `--dry-run` never changes settings or credentials.

Equivalent service URL overrides preserve the persisted native credential lookup
key without redirecting requests or copying secrets. Logout removes that same
entry. A different complete deployment URL cannot use the retained lookup key.
