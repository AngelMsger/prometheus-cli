// Command mockserver is a tiny stand-in for the Prometheus HTTP API, used by
// scripts/e2e.sh to exercise prometheus-cli end-to-end without a real
// Prometheus. It serves canned query, discovery, target, rule, alert, status
// and admin responses.
//
// It is served twice: unauthenticated at the root, mirroring a plain
// Prometheus, and behind a /secured prefix that requires a bearer token,
// mirroring a path-routed gateway. That lets the e2e suite cover both auth
// postures — and a base URL carrying a path — against one process.
//
// When MOCK_WRITE_LOG names a file, every request to the TSDB admin API (the
// only endpoints that change server state; queries are POSTed too, so the
// method alone does not say) is appended to it.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
)

// secureToken is the bearer token the /secured prefix demands.
const secureToken = "secret-token"

func main() {
	addr := "127.0.0.1:45090"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", route)
	var handler http.Handler = mux
	if path := os.Getenv("MOCK_WRITE_LOG"); path != "" {
		handler = logWrites(handler, path)
	}

	fmt.Fprintf(os.Stderr, "mockserver listening on %s\n", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// logWrites appends "METHOD PATH" to the file at path for every admin API
// request, so scripts/e2e.sh can assert that read-only mode let no write reach
// the server. A request that cannot be logged fails with 500 rather than going
// unrecorded.
func logWrites(next http.Handler, path string) http.Handler {
	var mu sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/v1/admin/") {
			mu.Lock()
			err := appendLine(path, r.Method+" "+r.URL.Path)
			mu.Unlock()
			if err != nil {
				http.Error(w, "mockserver: write log failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, line); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func route(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/releases/latest" {
		writeRaw(w, map[string]any{"tag_name": "v99.0.0", "html_url": "https://example/releases"})
		return
	}
	if rest, ok := strings.CutPrefix(path, "/secured"); ok {
		if r.Header.Get("Authorization") != "Bearer "+secureToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":"error","errorType":"unauthorized","error":"bearer token required"}`))
			return
		}
		path = rest
	}
	_ = r.ParseForm()

	switch {
	case path == "/api/v1/query":
		if bad := queryError(r); bad != nil {
			writeError(w, http.StatusBadRequest, bad)
			return
		}
		writeData(w, instantResult())
	case path == "/api/v1/query_range":
		if bad := queryError(r); bad != nil {
			writeError(w, http.StatusBadRequest, bad)
			return
		}
		writeEnvelope(w, map[string]any{
			"status":   "success",
			"data":     rangeResult(),
			"warnings": []string{"step is larger than the scrape interval"},
		})
	case path == "/api/v1/query_exemplars":
		writeData(w, exemplars())
	case path == "/api/v1/format_query":
		writeData(w, "sum by (job) (rate(up[5m]))")
	case path == "/api/v1/parse_query":
		writeData(w, map[string]any{"type": "aggregation", "op": "sum"})
	case path == "/api/v1/series":
		writeData(w, []map[string]string{
			{"__name__": "up", "job": "node", "instance": "node-1:9100"},
			{"__name__": "up", "job": "node", "instance": "node-2:9100"},
		})
	case path == "/api/v1/labels":
		writeData(w, []string{"__name__", "instance", "job"})
	case strings.HasPrefix(path, "/api/v1/label/") && strings.HasSuffix(path, "/values"):
		writeData(w, labelValues(path))
	case path == "/api/v1/metadata":
		writeData(w, map[string]any{
			"up": []map[string]string{{"type": "gauge", "help": "1 if the target is reachable", "unit": ""}},
			"http_requests_total": []map[string]string{
				{"type": "counter", "help": "Total HTTP requests", "unit": ""},
			},
		})
	case path == "/api/v1/targets/metadata":
		writeData(w, []map[string]any{{
			"target": map[string]string{"job": "node", "instance": "node-1:9100"},
			"metric": "up", "type": "gauge", "help": "1 if the target is reachable", "unit": "",
		}})
	case path == "/api/v1/targets":
		writeData(w, targets())
	case path == "/api/v1/rules":
		writeData(w, rules())
	case path == "/api/v1/alerts":
		writeData(w, map[string]any{"alerts": []map[string]any{{
			"labels":      map[string]string{"alertname": "HighRequestLatency", "severity": "page"},
			"annotations": map[string]string{"summary": "High request latency"},
			"state":       "firing", "activeAt": "2026-09-19T20:27:12Z", "value": "1e+00",
		}}})
	case path == "/api/v1/alertmanagers":
		writeData(w, map[string]any{
			"activeAlertmanagers":  []map[string]string{{"url": "http://alertmanager:9093/api/v2/alerts"}},
			"droppedAlertmanagers": []map[string]string{},
		})
	case path == "/api/v1/status/buildinfo":
		writeData(w, map[string]any{"version": "3.1.0", "revision": "abc123", "goVersion": "go1.24.0"})
	case path == "/api/v1/status/config":
		writeData(w, map[string]any{"yaml": "global:\n  scrape_interval: 15s\n"})
	case path == "/api/v1/status/flags":
		writeData(w, map[string]string{
			"storage.tsdb.retention.time": "15d",
			"web.enable-admin-api":        "true",
		})
	case path == "/api/v1/status/runtimeinfo":
		writeData(w, map[string]any{"startTime": "2026-09-19T00:00:00Z", "timeSeriesCount": 1200})
	case path == "/api/v1/status/tsdb":
		writeData(w, map[string]any{
			"headStats": map[string]any{"numSeries": 1200, "numLabelPairs": 400},
			"seriesCountByMetricName": []map[string]any{
				{"name": "http_requests_total", "value": 800},
			},
		})
	case path == "/api/v1/status/walreplay":
		writeData(w, map[string]any{"min": 0, "max": 0, "current": 0, "state": "done"})
	case path == "/api/v1/status/notifications":
		writeData(w, []map[string]any{})
	case path == "/api/v1/admin/tsdb/delete_series":
		w.WriteHeader(http.StatusNoContent)
	case path == "/api/v1/admin/tsdb/snapshot":
		writeData(w, map[string]any{"name": "20260920T101112Z-1a2b3c"})
	case path == "/api/v1/admin/tsdb/clean_tombstones":
		// Stands in for a server started without --web.enable-admin-api, so the
		// suite can assert the ADMIN_API_DISABLED translation.
		writeError(w, http.StatusNotFound, map[string]any{
			"status": "error", "errorType": "not_found", "error": "admin APIs are disabled",
		})
	default:
		writeError(w, http.StatusNotFound, map[string]any{
			"status": "error", "errorType": "not_found", "error": "no such endpoint " + path,
		})
	}
}

// queryError returns a bad_data envelope when the expression is the canned
// invalid one, so the suite can assert PromQL failures classify as usage.
func queryError(r *http.Request) map[string]any {
	if !strings.Contains(r.FormValue("query"), "not_a_function(") {
		return nil
	}
	return map[string]any{
		"status": "error", "errorType": "bad_data",
		"error": `invalid parameter "query": 1:1: parse error: unknown function with name "not_a_function"`,
	}
}

func instantResult() map[string]any {
	return map[string]any{
		"resultType": "vector",
		"result": []map[string]any{
			{"metric": map[string]string{"__name__": "up", "job": "node", "instance": "node-1:9100"},
				"value": []any{1758326400.5, "1"}},
			{"metric": map[string]string{"__name__": "up", "job": "node", "instance": "node-2:9100"},
				"value": []any{1758326400.5, "0"}},
		},
	}
}

func rangeResult() map[string]any {
	return map[string]any{
		"resultType": "matrix",
		"result": []map[string]any{
			{"metric": map[string]string{"__name__": "up", "job": "node"},
				"values": []any{
					[]any{1758322800, "1"},
					[]any{1758326400, "0"},
				}},
		},
	}
}

func exemplars() []map[string]any {
	return []map[string]any{{
		"seriesLabels": map[string]string{"__name__": "http_request_duration_seconds_bucket", "job": "api"},
		"exemplars": []map[string]any{{
			"labels": map[string]string{"trace_id": "deadbeef"}, "value": "0.42", "timestamp": 1758326400.1,
		}},
	}}
}

func targets() map[string]any {
	return map[string]any{
		"activeTargets": []map[string]any{
			{"scrapePool": "node", "scrapeUrl": "http://node-1:9100/metrics", "health": "up",
				"labels":           map[string]string{"job": "node", "instance": "node-1:9100"},
				"discoveredLabels": map[string]string{"__address__": "node-1:9100", "job": "node"},
				"lastScrape":       "2026-09-20T10:00:00Z", "lastScrapeDuration": 0.012,
				"scrapeInterval": "15s", "scrapeTimeout": "10s"},
			{"scrapePool": "node", "scrapeUrl": "http://node-2:9100/metrics", "health": "down",
				"labels":           map[string]string{"job": "node", "instance": "node-2:9100"},
				"discoveredLabels": map[string]string{"__address__": "node-2:9100", "job": "node"},
				"lastError":        "connection refused",
				"lastScrape":       "2026-09-20T10:00:00Z", "lastScrapeDuration": 0.001,
				"scrapeInterval": "15s", "scrapeTimeout": "10s"},
		},
		"droppedTargets": []map[string]any{
			{"discoveredLabels": map[string]string{"__address__": "retired:9100", "job": "retired"}},
		},
	}
}

func rules() map[string]any {
	return map[string]any{
		"groups": []map[string]any{{
			"name": "node-alerts", "file": "/etc/prometheus/rules.yml", "interval": 60,
			"rules": []map[string]any{
				{"type": "alerting", "name": "HighRequestLatency", "query": "latency > 0.5",
					"health": "ok", "state": "firing", "duration": 600,
					"labels":      map[string]string{"severity": "page"},
					"annotations": map[string]string{"summary": "High request latency"},
					"alerts": []map[string]any{{
						"labels":      map[string]string{"alertname": "HighRequestLatency", "severity": "page"},
						"annotations": map[string]string{"summary": "High request latency"},
						"state":       "firing", "activeAt": "2026-09-19T20:27:12Z", "value": "1e+00",
					}},
					"evaluationTime": 0.002, "lastEvaluation": "2026-09-20T10:00:00Z"},
				{"type": "recording", "name": "job:requests:rate5m", "query": "sum by (job) (rate(http_requests_total[5m]))",
					"health": "err", "lastError": "found duplicate series for the match group",
					"evaluationTime": 0.004, "lastEvaluation": "2026-09-20T10:00:00Z"},
			},
		}},
	}
}

func labelValues(path string) []string {
	name := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/label/"), "/values")
	if name == "__name__" {
		return []string{"http_requests_total", "up"}
	}
	return []string{"node", "api"}
}

func writeData(w http.ResponseWriter, data any) {
	writeEnvelope(w, map[string]any{"status": "success", "data": data})
}

func writeEnvelope(w http.ResponseWriter, env map[string]any) {
	writeRaw(w, env)
}

func writeError(w http.ResponseWriter, status int, env map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

func writeRaw(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
