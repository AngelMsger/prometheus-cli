package prometheuscli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// Exercise the actual process boundary: data and notices must stay on their
// respective streams, and invalid bounds must not result in network traffic.
func TestCLIResponseIntegrity(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "prometheus-cli")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, "./cmd/prometheus-cli").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PROMETHEUS_") {
			env = append(env, entry)
		}
	}
	env = append(env, "PROMETHEUS_CLI_NO_SKILL_HINT=1", "PROMETHEUS_CLI_NO_UPDATE_NOTIFIER=1", "PROMETHEUS_CLI_READ_ONLY=1")
	run := func(t *testing.T, url string, wantExit int, args ...string) (string, string) {
		t.Helper()
		argv := append([]string{"--config", filepath.Join(dir, "config"), "--base-url", url, "--auth-scheme", "none"}, args...)
		cmd := exec.Command(binary, argv...)
		cmd.Env, cmd.Dir = env, dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if code != wantExit {
			t.Fatalf("exit %d, want %d; stderr: %s", code, wantExit, stderr.String())
		}
		if wantExit != 0 {
			if stdout.Len() != 0 || !json.Valid(stderr.Bytes()) {
				t.Fatalf("invalid error streams: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
		}
		return stdout.String(), stderr.String()
	}

	t.Run("discovery advisories in all formats", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := `["job"]`
			if r.URL.Path == "/api/v1/series" {
				data = `[{"__name__":"up"}]`
			}
			_, _ = w.Write([]byte(`{"status":"success","data":` + data + `,"warnings":["results truncated due to limit"],"infos":["partial index"]}`))
		}))
		defer server.Close()
		for _, args := range [][]string{{"series", "list", "--match", "up"}, {"labels", "list"}, {"labels", "values", "job"}} {
			for _, format := range []string{"json", "table", "ndjson"} {
				stdout, stderr := run(t, server.URL, 0, append(args, "--limit", "1", "--format", format)...)
				if strings.Contains(stdout, "truncated") || !strings.Contains(stderr, `"api_advisories"`) || !strings.Contains(stderr, "results truncated due to limit") || !strings.Contains(stderr, "partial index") {
					t.Fatalf("lost or misplaced advisory: stdout=%s stderr=%s", stdout, stderr)
				}
				if !json.Valid([]byte(stderr)) {
					t.Fatalf("notice is not JSON: %s", stderr)
				}
			}
		}
	})

	// A query result carries its advisories inside the document. Projecting
	// the document away from them must not hide a truncation warning.
	t.Run("query advisories survive field projection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"__name__":"up"},"value":[1000,"1"]}]},"warnings":["results truncated due to limit"]}`))
		}))
		defer server.Close()
		for _, format := range []string{"json", "table"} {
			stdout, stderr := run(t, server.URL, 0, "query", "instant", "--query", "up", "--limit", "1", "--fields", "series_count", "--format", format)
			if strings.Contains(stdout, "truncated") || !strings.Contains(stderr, `"query_advisories"`) || !strings.Contains(stderr, "results truncated due to limit") {
				t.Fatalf("%s: projection hid the advisory: stdout=%s stderr=%s", format, stdout, stderr)
			}
			if !json.Valid([]byte(stderr)) || !strings.Contains(stderr, `"next_steps"`) || !strings.Contains(stderr, "--limit") {
				t.Fatalf("%s: advisory lacks a recovery path: %s", format, stderr)
			}
		}
		stdout, stderr := run(t, server.URL, 0, "query", "instant", "--query", "up", "--limit", "1")
		if !strings.Contains(stdout, "results truncated due to limit") || stderr != "" {
			t.Fatalf("an unprojected document must carry the advisory once: stdout=%s stderr=%s", stdout, stderr)
		}
	})

	t.Run("empty reads cannot report success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		defer server.Close()
		for _, args := range [][]string{{"query", "instant", "--query", "up"}, {"status", "buildinfo"}} {
			_, stderr := run(t, server.URL, 10, args...)
			if !strings.Contains(stderr, "NOT_PROMETHEUS_API") {
				t.Fatal(stderr)
			}
		}
		stdout, _ := run(t, server.URL, 0, "auth", "status")
		var status struct {
			Authenticated bool   `json:"authenticated"`
			Error         string `json:"error"`
		}
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
			t.Fatal(err)
		}
		if status.Authenticated || status.Error == "" {
			t.Fatalf("false authentication success: %s", stdout)
		}
	})

	t.Run("invalid bounds stay offline", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
		defer server.Close()
		for _, args := range [][]string{
			{"query", "instant", "--query", "up", "--limit", "-1"},
			{"query", "range", "--query", "up", "--since", "5m", "--limit", "-1"},
			{"query", "instant", "--query", "up", "--query-timeout", "-1s"},
			{"query", "instant", "--query", "up", "--query-timeout", "0s"},
			{"query", "range", "--query", "up", "--since", "5m", "--query-timeout", "-1s"},
			{"series", "list", "--match", "up", "--limit", "-1"},
			{"labels", "list", "--limit", "-1"},
			{"labels", "values", "job", "--limit", "-1"},
			{"metadata", "list", "--limit", "-1"},
			{"metadata", "list", "--limit-per-metric", "-1"},
			{"metadata", "targets", "--limit", "-1"},
			{"rule", "list", "--group-limit", "-1"},
			{"status", "tsdb", "--limit", "-1"},
		} {
			run(t, server.URL, 2, args...)
		}
		if requests.Load() != 0 {
			t.Fatalf("sent %d invalid requests", requests.Load())
		}
	})

	// Every command that takes the window flags shares one parser, so each
	// must refuse an ambiguous window — --since beside --from/--to, or --to
	// with no --from — before it can query, or delete, a window the caller did
	// not ask for.
	t.Run("ambiguous windows stay offline", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
		defer server.Close()
		commands := [][]string{
			{"query", "range", "--query", "up"},
			{"query", "exemplars", "--query", "up"},
			{"series", "list", "--match", "up"},
			{"labels", "list"},
			{"labels", "values", "job"},
			{"admin", "delete-series", "--match", "up", "--dry-run"},
			{"--allow-writes", "admin", "delete-series", "--match", "up", "--yes"},
		}
		windows := []struct {
			flags []string
			want  string
		}{
			{[]string{"--since", "1h", "--from", "2026-09-01T00:00:00Z"}, "--since cannot be combined with --from or --to"},
			{[]string{"--since", "1h", "--to", "2026-09-02T00:00:00Z"}, "--since cannot be combined with --from or --to"},
			{[]string{"--since", "1h", "--from", "2026-09-01T00:00:00Z", "--to", "2026-09-02T00:00:00Z"}, "--since cannot be combined with --from or --to"},
			{[]string{"--to", "2026-09-02T00:00:00Z"}, "--to requires --from"},
		}
		for _, command := range commands {
			for _, window := range windows {
				args := append(append([]string{}, command...), window.flags...)
				_, stderr := run(t, server.URL, 2, args...)
				var failure struct {
					Error struct {
						Category  string   `json:"category"`
						Code      string   `json:"code"`
						Message   string   `json:"message"`
						Hint      string   `json:"hint"`
						NextSteps []string `json:"next_steps"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(stderr), &failure); err != nil {
					t.Fatalf("%v: %v: %s", args, err, stderr)
				}
				e := failure.Error
				if e.Category != "usage" || e.Code != "BAD_TIME_RANGE" || !strings.Contains(e.Message, window.want) || e.Hint == "" || len(e.NextSteps) == 0 {
					t.Fatalf("%v: unexpected error: %s", args, stderr)
				}
			}
		}
		if requests.Load() != 0 {
			t.Fatalf("sent %d requests for ambiguous windows", requests.Load())
		}
	})

	t.Run("NDJSON exposes a usable cursor", func(t *testing.T) {
		var resumed atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("group_next_token") == "opaque-cursor" {
				resumed.Store(true)
				_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"groups":[],"groupNextToken":"opaque-cursor"}}`))
		}))
		defer server.Close()
		stdout, stderr := run(t, server.URL, 0, "rule", "list", "--group-limit", "1", "--format", "ndjson")
		var notice struct {
			Notice struct {
				Pagination struct {
					Next    string `json:"next"`
					HasMore bool   `json:"has_more"`
				} `json:"pagination"`
			} `json:"_notice"`
		}
		if err := json.Unmarshal([]byte(stderr), &notice); err != nil {
			t.Fatal(err)
		}
		if stdout != "" || !notice.Notice.Pagination.HasMore || notice.Notice.Pagination.Next == "" {
			t.Fatalf("lost cursor: stdout=%s stderr=%s", stdout, stderr)
		}
		_, stderr = run(t, server.URL, 0, "rule", "list", "--group-limit", "1", "--cursor", notice.Notice.Pagination.Next, "--format", "ndjson")
		if !resumed.Load() || stderr != "" {
			t.Fatalf("resume failed: %s", stderr)
		}
	})

	t.Run("empty string value reaches stdout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"string","result":[1000,""]}}`))
		}))
		defer server.Close()
		stdout, _ := run(t, server.URL, 0, "query", "instant", "--query", `""`)
		var result struct {
			Series []struct {
				Value map[string]any `json:"value"`
			} `json:"series"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Series) != 1 || result.Series[0].Value["value"] != "" {
			t.Fatalf("empty string lost: %s", stdout)
		}
	})
}
