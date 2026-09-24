#!/usr/bin/env bash
# End-to-end smoke test: run the built binary against the mock Prometheus
# server and assert the agent-facing contract holds (JSON output, normalized
# samples, structured errors, exit codes, write safety). No real credentials or
# server required.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SKILL_VERSION="$(sed -n 's/^version: *//p' "$ROOT/skills/prometheus/SKILL.md" | head -1)"
BIN="$ROOT/bin/prometheus-cli"
ADDR="127.0.0.1:45090"
URL="http://$ADDR"

LDFLAGS="-X github.com/angelmsger/prometheus-cli/pkg/constants.Version=0.0.1"
(cd "$ROOT" && go build -ldflags "$LDFLAGS" -o "$BIN" ./cmd/prometheus-cli)

TMP="$(mktemp -d)"
cleanup() {
  [[ -n "${MOCK_PID:-}" ]] && kill "$MOCK_PID" 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

# Build and start the mock server (build to a binary so the trap can kill the
# actual server process — `go run` would leave its compiled child orphaned).
go build -o "$TMP/mockserver" "$ROOT/test/mockserver"
"$TMP/mockserver" "$ADDR" 2>"$TMP/mock.log" &
MOCK_PID=$!

# Wait for it to accept connections.
for _ in $(seq 1 50); do
  if curl -fsS -o /dev/null "$URL/api/v1/status/buildinfo" 2>/dev/null; then
    break
  fi
  sleep 0.1
done

export PROMETHEUS_URL="$URL"
export PROMETHEUS_RELEASE_API="$URL/releases/latest"
export PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1
export PROMETHEUS_CLI_SKILL="$SKILL_VERSION"

run() { "$BIN" --config "$TMP" "$@"; }

pass=0
check() { # check <label> <expected-substr> -- <command...>
  local label="$1" want="$2"; shift 3
  local out; out="$("$@" 2>/dev/null || true)"
  if grep -q "$want" <<<"$out"; then
    echo "ok   - $label"
    pass=$((pass + 1))
  else
    echo "FAIL - $label (wanted: $want)"
    echo "$out" | head -5
    exit 1
  fi
}

# --- query: the core surface -------------------------------------------------
check "query instant"          '"result_type": "vector"'   -- run query instant --query up
check "instant sample decoded" '"value": "1"'              -- run query instant --query up
check "instant time rendered"  '"time": "2025-09-20T'      -- run query instant --query up
check "series count"           '"series_count": 2'         -- run query instant --query up
check "query range"            '"result_type": "matrix"'   -- run query range --query up --since 1h
check "range derives a step"   '"source": "derived"'       -- run query range --query up --since 1h
check "range echoes warnings"  'scrape interval'           -- run query range --query up --since 1h
check "explicit step kept"     '"source": "flag"'          -- run query range --query up --since 1h --step 5m
check "oversized step clamped" '"source": "clamped"'       -- run query range --query up --since 30d --step 1s
check "query from stdin"       '"result_type": "vector"'   -- sh -c "echo up | $BIN --config $TMP query instant --query @-"
check "exemplars"              'deadbeef'                  -- run query exemplars --query bucket --since 1h
check "format query"           '"formatted"'               -- run query format --query 'sum by(job)(rate(up[5m]))'
check "parse query"            '"ast"'                     -- run query parse --query up

# --- discovery ---------------------------------------------------------------
check "series list"     '"instance": "node-1:9100"' -- run series list --match up --since 1h
check "labels list"     '"job"'                     -- run labels list --since 1h
check "label values"    'http_requests_total'       -- run labels values __name__ --since 1h
check "metadata list"   '"type": "counter"'         -- run metadata list
check "metadata sorted" '"metric": "http_requests_total"' -- run metadata list
check "target metadata" '"metric": "up"'            -- run metadata targets --metric up

# --- operations --------------------------------------------------------------
check "target list"          '"state": "dropped"'       -- run target list
check "target relative time" '"last_scrape_ago"'        -- run target list
check "unhealthy filter"     'connection refused'       -- run target list --unhealthy
check "rules flattened"      '"group": "node-alerts"'   -- run rule list
check "failing rules"        'duplicate series'         -- run rule list --failing
check "alert list"           '"state": "firing"'        -- run alert list
check "alert active_for"     '"active_for"'             -- run alert list
check "alert name filter"    'HighRequestLatency'       -- run alert list --name HighRequestLatency
check "alertmanagers"        'alertmanager:9093'        -- run alert managers
check "status flags"         'web.enable-admin-api'     -- run status flags
check "status tsdb"          '"numSeries"'              -- run status tsdb
check "status config yaml"   'scrape_interval'          -- run status config
check "auth status"          '"authenticated": true'    -- run auth status
check "auth status version"  '"server_version": "3.1.0"' -- run auth status
check "doctor healthy"       '"healthy": true'          -- run doctor --no-update-check
check "doctor names scheme none" 'no credential required' -- run doctor --no-update-check
check "doctor reports Skill" '"companion-skill"'        -- run doctor --no-update-check

# --- output contract ---------------------------------------------------------
check "ndjson streams series" '"metric"' -- run --format ndjson query instant --query up
if [[ "$(run --format ndjson query instant --query up 2>/dev/null | wc -l | tr -d ' ')" != "2" ]]; then
  echo "FAIL - ndjson emits one record per series"; exit 1
fi
echo "ok   - ndjson emits one record per series"
pass=$((pass + 1))
check "field projection on a stream" '"metric.job"' -- \
  run --format ndjson --fields metric.job query instant --query up
check "field projection on a list" '"state"' -- run --fields state,health target list

# --- authenticated, path-routed gateway --------------------------------------
check "bearer auth over a path-routed base URL" '"result_type": "vector"' -- \
  env PROMETHEUS_URL="$URL/secured" PROMETHEUS_TOKEN="secret-token" \
      PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 "$BIN" --config "$TMP" query instant --query up

set +e
env PROMETHEUS_URL="$URL/secured" PROMETHEUS_TOKEN="wrong" PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 \
  "$BIN" --config "$TMP" query instant --query up >/dev/null 2>&1; code=$?
set -e
if [[ "$code" -ne 4 ]]; then
  echo "FAIL - a rejected token should exit 4, got $code"; exit 1
fi
echo "ok   - a rejected token exits 4"
pass=$((pass + 1))

# --- errors and exit codes ---------------------------------------------------
set +e
bad="$(run query instant --query 'not_a_function(up)' 2>&1 >/dev/null)"; code=$?
set -e
if [[ "$code" -ne 2 ]] || ! grep -q '"category": "usage"' <<<"$bad"; then
  echo "FAIL - a PromQL error should be a usage error (exit 2), got $code"; echo "$bad" | head -5; exit 1
fi
echo "ok   - a PromQL error exits 2 as a usage error"
pass=$((pass + 1))
check "PromQL error points at discovery" 'metadata list' -- \
  sh -c "$BIN --config $TMP query instant --query 'not_a_function(up)' 2>&1 >/dev/null"

set +e
run query range --query up >/dev/null 2>&1; code=$?
set -e
if [[ "$code" -ne 2 ]]; then
  echo "FAIL - a range query without a window should exit 2, got $code"; exit 1
fi
echo "ok   - a range query without a window exits 2"
pass=$((pass + 1))

set +e
run status nosuchtopic >/dev/null 2>&1; code=$?
set -e
if [[ "$code" -ne 2 ]]; then
  echo "FAIL - an unknown status topic should exit 2, got $code"; exit 1
fi
echo "ok   - an unknown status topic exits 2"
pass=$((pass + 1))

# --- writes: preview, confirmation, read-only --------------------------------
check "delete-series dry-run"   '"dry_run": true'  -- run admin delete-series --match 'up{job="retired"}' --dry-run
check "dry-run states effect"   '"effect"'         -- run admin delete-series --match 'up{job="retired"}' --dry-run
check "snapshot dry-run"        'admin/tsdb/snapshot' -- run admin snapshot --dry-run

set +e
run admin delete-series --match up >/dev/null 2>&1; code=$?
set -e
if [[ "$code" -ne 2 ]]; then
  echo "FAIL - a destructive write without --yes should exit 2, got $code"; exit 1
fi
echo "ok   - a destructive write without --yes exits 2"
pass=$((pass + 1))

check "delete-series executes with --yes" '"deleted": true' -- run admin delete-series --match up --yes
check "snapshot executes"                 '"name"'          -- run admin snapshot

set +e
PROMETHEUS_CLI_READ_ONLY=1 run admin delete-series --match up --yes >/dev/null 2>&1; code=$?
set -e
if [[ "$code" -ne 5 ]]; then
  echo "FAIL - read-only write should exit 5, got $code"; exit 1
fi
echo "ok   - read-only blocks admin delete-series (exit 5)"
pass=$((pass + 1))

check "read-only still previews" '"dry_run": true' -- \
  env PROMETHEUS_CLI_READ_ONLY=1 PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 \
      "$BIN" --config "$TMP" admin delete-series --match up --dry-run
check "allow-writes overrides read-only" '"deleted": true' -- \
  env PROMETHEUS_CLI_READ_ONLY=1 PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 \
      "$BIN" --config "$TMP" --allow-writes admin delete-series --match up --yes

set +e
out="$(run admin clean-tombstones --yes 2>&1 >/dev/null)"; code=$?
set -e
if [[ "$code" -ne 5 ]] || ! grep -q 'ADMIN_API_DISABLED' <<<"$out"; then
  echo "FAIL - a disabled admin API should surface ADMIN_API_DISABLED (exit 5), got $code"
  echo "$out" | head -5; exit 1
fi
echo "ok   - a disabled admin API reports ADMIN_API_DISABLED (exit 5)"
pass=$((pass + 1))

# --- argv normalization ------------------------------------------------------
check "camelCase flag corrected" '"corrected":"--scrape-pool"' -- \
  sh -c "$BIN --config $TMP target list --scrapePool node 2>&1 >/dev/null"
check "sticky value split" '"corrected":"--limit 5"' -- \
  sh -c "$BIN --config $TMP labels list --limit5 2>&1 >/dev/null"

# --- Skill handshake ---------------------------------------------------------
SKILL_HOME="$TMP/skill-home"
mkdir -p "$SKILL_HOME"
check "skill install for Codex" '"alignment": "current"' -- \
  env HOME="$SKILL_HOME" "$BIN" --config "$TMP" skill install --agent codex
check "skill status version aligned" '"loaded_status": "current"' -- \
  env HOME="$SKILL_HOME" PROMETHEUS_CLI_SKILL="$SKILL_VERSION" "$BIN" --config "$TMP" skill status
legacy_out="$(env HOME="$SKILL_HOME" PROMETHEUS_CLI_SKILL=1 PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1 \
  "$BIN" --config "$TMP" labels list 2>&1 || true)"
if grep -q '"status":"unknown"' <<<"$legacy_out"; then
  echo "ok   - legacy Skill handshake is detected"
  pass=$((pass + 1))
else
  echo "FAIL - legacy Skill handshake is detected"; exit 1
fi
update_out="$(env -u PROMETHEUS_CLI_NO_UPDATE_NOTIFIER PROMETHEUS_CLI_SKILL="$SKILL_VERSION" \
  "$BIN" --config "$TMP" labels list 2>&1 || true)"
if grep -q '"next_steps"' <<<"$update_out" && grep -q 'prometheus-cli skill install' <<<"$update_out"; then
  echo "ok   - update notice includes Skill refresh"
  pass=$((pass + 1))
else
  echo "FAIL - update notice includes Skill refresh"; exit 1
fi

# --- config init -------------------------------------------------------------
# A fresh setup with the default `none` scheme needs only a URL, then a re-run
# that must announce the existing config and ask edit/add/replace — here driving
# the "add" path to a second context.
CFG="$(mktemp -d)"
printf '%s\nnone\n\n' "$URL" | "$BIN" --config "$CFG" config init >/dev/null 2>&1
printf 'add\nprod\n%s\nnone\n\n' "$URL" | "$BIN" --config "$CFG" config init >/dev/null 2>"$CFG/err"
if grep -q "edit/add/replace" "$CFG/err" \
   && "$BIN" --config "$CFG" config contexts 2>/dev/null | grep -q '"name": "prod"'; then
  echo "ok   - config init asks edit/add/replace and adds a context"
  pass=$((pass + 1))
else
  echo "FAIL - config init add-context flow"; head -5 "$CFG/err"; exit 1
fi
if [[ -e "$CFG/credentials" ]]; then
  echo "FAIL - the none scheme must store no credential"; exit 1
fi
echo "ok   - the none scheme stores no credential"
pass=$((pass + 1))
rm -rf "$CFG"

echo ""
echo "e2e: $pass checks passed"

"$ROOT/scripts/e2e-setup.sh"
