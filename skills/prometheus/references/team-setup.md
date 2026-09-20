# Team setup reference

## Distribution and login

Distribute service settings separately from each member's credentials. An
installer can write a named context without a network connection or access to
the keychain:

```bash
# an internal Prometheus with no authentication — this is the whole setup
prometheus-cli config set-context team \
  --base-url https://prometheus.example.com --auth-scheme none --activate

# a multi-tenant backend behind a gateway
prometheus-cli config set-context mimir \
  --base-url https://mimir.example.com --auth-scheme bearer --tenant team-a

# the member completes personal authentication in a terminal
prometheus-cli --use-context mimir auth guide
prometheus-cli --use-context mimir auth login
```

`config set-context <name>` resolves **flags > environment > `.env` > the named
target context > defaults**. It ignores personal environment fields and secrets,
including secret-based scheme inference. It never verifies connectivity, reads
or writes the keychain, or changes another context's values or shared defaults.
Existing usernames remain unchanged. The first context becomes current;
subsequent calls change the current context only with `--activate`.

The **tenant id is a service preset, not an identity**: everyone on the team
queries the same multi-tenant backend under the same `X-Scope-OrgID`, so it is
distributed with the URL and scheme rather than collected per person.

A pasted API endpoint is trimmed back to the server root before it is stored, so
`https://prom.example.com/api/v1` and `https://prom.example.com` produce the
same context.

Identical presets do not rewrite the file. Conflicting non-empty service fields
return `CONFIG_CONTEXT_CONFLICT` with a `details` object containing each field's
`before` and `after` values. Inspect those differences, then use `--overwrite`
to update the supplied service fields, or use another context name. Unspecified
fields are retained. `--dry-run` uses the same merge and conflict checks and
returns the proposed changes without writing anything; use `--overwrite
--dry-run` to preview a deliberate conflicting update.

## Credential guidance

`auth guide` works offline and emits `server`, `scheme`, `credential_url`,
`source`, `instructions`, `documentation_url`, and `next_steps`. Sources are
`flag`, `env`, `dotenv`, `file`, `builtin`, or `fallback`.

Prometheus has **no credential page of its own**, so with nothing configured the
guide falls back to the server root and reports `"source": "fallback"` rather
than naming a token URL that does not exist. The scheme decides the guidance:

- `none` — nothing to acquire. `next_steps` points at `doctor`, and `auth login`
  on such a context returns `AUTH_NOT_REQUIRED`.
- `basic` — the credential comes from whatever fronts Prometheus (a reverse
  proxy, an ingress, or its own `web.yml` `basic_auth_users`). Set
  `--credential-url` to your team's own credential page so the guide points
  there.
- `bearer` — the credential comes from the hosting product's console (Grafana
  Cloud, Thanos, Cortex/Mimir) or a Kubernetes service account.

There is no server-version probe; links are hints, not evidence of server
capabilities.

## Personal login

Personal login checks the complete normalized service URL before storing a
secret, then records its username, scheme and tenant so the next process can
resolve it. A URL mismatch returns `CONTEXT_BASE_URL_MISMATCH`; select or create
a matching context. A `LOGIN_CONFIG_WRITE_FAILED` error reports
`credential_stored: true` and its server/context/scheme; fix file access and
repeat personal login. Never replace an inaccessible host credential with new
setup: follow its host recovery first.

Verification is a build-info read. Prometheus exposes no identity endpoint, so
there is no authenticated user to assert — a successful read proves the URL is a
Prometheus API and that the gateway accepted what the credential sends, and that
is the strongest available check.

Service variables can be injected by the user's shell, launcher, or CI:
`PROMETHEUS_URL`, `PROMETHEUS_AUTH_SCHEME`, `PROMETHEUS_TENANT` and optionally
`PROMETHEUS_CREDENTIAL_URL`. Environment credentials remain transient. Do not
collect credentials through chat or pass them in command arguments. See the
[installation guide](https://github.com/AngelMsger/prometheus-cli/blob/main/docs/installation.md#team-distribution-and-personal-login)
for distribution examples and the full recovery contract.
