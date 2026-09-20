#!/usr/bin/env bash
# Exercise team distribution without a running service or access to personal state.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/prometheus-cli"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"
run() {
  env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 \
    "$BIN" --config "$WORK/config" "$@"
}
require() { [[ "$1" == *"$2"* ]] || { echo "FAIL: expected $2" >&2; exit 1; }; }

out="$(run config set-context team --base-url https://offline.invalid/prom --auth-scheme bearer --dry-run)"
require "$out" '"dry_run": true'
[[ ! -e "$WORK/config/config.yaml" ]]

# Personal identity and secrets in the environment must not reach a team preset.
out="$(env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 PROMETHEUS_CONTEXT=unconfigured \
  PROMETHEUS_TOKEN=do-not-save PROMETHEUS_USER=do-not-save \
  "$BIN" --config "$WORK/config" config set-context team --base-url https://offline.invalid/prom \
  --auth-scheme bearer --tenant team-a --credential-url https://help.invalid/team-tokens)"
require "$out" '"current_context": "team"'
[[ "$(cat "$WORK/config/config.yaml")" != *do-not-save* ]]
[[ ! -e "$WORK/config/credentials" ]]
require "$(cat "$WORK/config/config.yaml")" 'tenant_id: team-a'

out="$(run config set-context team)"; require "$out" '"changed": false'
out="$(run auth guide)"
require "$out" 'https://help.invalid/team-tokens'
require "$out" '"source": "file"'
require "$out" "--use-context 'team' auth login"

# A pasted API endpoint is trimmed back to the server root rather than stored.
run config set-context endpoint --base-url https://offline.invalid/prom/api/v1 --auth-scheme none >/dev/null
out="$(run --use-context endpoint config show)"
require "$out" 'https://offline.invalid/prom'
[[ "$out" != *'/api/v1'* ]] || { echo 'FAIL: /api/v1 was stored in the base URL' >&2; exit 1; }

# A `none` context points at doctor, not at a login that would store nothing.
out="$(run config set-context plain --base-url https://plain.invalid --auth-scheme none)"
require "$out" "--use-context 'plain' doctor"

if run config set-context team --base-url https://other.invalid >out.json 2>error.json; then
  echo 'FAIL: conflicting setup succeeded' >&2; exit 1
fi
[[ ! -s out.json ]]
require "$(cat error.json)" CONFIG_CONTEXT_CONFLICT
require "$(cat error.json)" '"details"'
run config set-context team --base-url https://other.invalid --overwrite >/dev/null
run config set-context second --base-url https://second.invalid --auth-scheme none >/dev/null
out="$(run --use-context team config show)"; require "$out" 'https://other.invalid'
run config set-context second --activate >/dev/null
out="$(run config show)"; require "$out" 'https://second.invalid'

# An environment-only consumer receives the same acquisition guidance without a config file.
out="$(env -i PATH="$PATH" HOME="$WORK" NO_COLOR=1 PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 \
  PROMETHEUS_URL=https://env.invalid/prom PROMETHEUS_AUTH_SCHEME=bearer \
  PROMETHEUS_CREDENTIAL_URL=https://help.invalid/env \
  "$BIN" --config "$WORK/empty" auth guide)"
require "$out" 'https://help.invalid/env'
require "$out" '"source": "env"'
[[ ! -e "$WORK/empty/config.yaml" ]]

# `auth login` on a none context refuses rather than storing an empty secret.
if run --use-context plain auth login >out.json 2>error.json; then
  echo 'FAIL: auth login succeeded on a none context' >&2; exit 1
fi
require "$(cat error.json)" AUTH_NOT_REQUIRED

echo 'PASS: offline team setup, idempotency, conflict, activation and environment-only guidance'
