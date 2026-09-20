package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// newTestClient wires a Client to a handler, with no auth decorator — the
// posture of a plain Prometheus.
func newTestClient(t *testing.T, handler http.HandlerFunc) (Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := BuildClient(BuildParams{BaseURL: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client, srv
}

// serveData replies with a success envelope carrying data.
func serveData(data string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":` + data + `}`))
	}
}

func TestQueryNormalizesVectorSamples(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"resultType":"vector","result":[
		{"metric":{"__name__":"up","job":"node"},"value":[1758326400.5,"1"]}]}`))

	got, err := client.Query(context.Background(), InstantRequest{Query: "up"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ResultType != "vector" || got.SeriesCount != 1 {
		t.Fatalf("got %+v", got)
	}
	s := got.Series[0]
	if s.Name != "up" || s.Metric["job"] != "node" {
		t.Fatalf("labels not lifted: %+v", s)
	}
	if s.Value == nil || s.Value.Value != "1" || s.Value.Timestamp != 1758326400.5 {
		t.Fatalf("sample tuple not decoded: %+v", s.Value)
	}
	if s.Value.Time != "2025-09-20T00:00:00.5Z" {
		t.Fatalf("instant not rendered: %q", s.Value.Time)
	}
}

func TestQueryRangeNormalizesMatrixAndCarriesWarnings(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","warnings":["partial response"],
			"data":{"resultType":"matrix","result":[
				{"metric":{"__name__":"up"},"values":[[1,"1"],[2,"0"]]}]}}`))
	})
	start := time.Unix(0, 0)
	got, err := client.QueryRange(context.Background(), RangeRequest{
		Query: "up", Start: start, End: start.Add(time.Hour), Step: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Series) != 1 || len(got.Series[0].Values) != 2 {
		t.Fatalf("got %+v", got.Series)
	}
	if got.Series[0].Values[1].Value != "0" {
		t.Fatalf("second sample = %+v", got.Series[0].Values[1])
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("server warnings were dropped: %+v", got.Warnings)
	}
	if got.Start == "" || got.End == "" {
		t.Fatal("range bounds were not echoed back")
	}
}

// A scalar or string result is a bare tuple, not a list, and must not be
// silently dropped for having a different shape.
func TestQueryNormalizesScalarResults(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"resultType":"scalar","result":[1758326400,"42"]}`))
	got, err := client.Query(context.Background(), InstantRequest{Query: "scalar(up)"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SeriesCount != 1 || got.Series[0].Value == nil || got.Series[0].Value.Value != "42" {
		t.Fatalf("got %+v", got.Series)
	}
}

// Native histogram samples carry an object instead of a float; keeping the body
// verbatim means nothing the server sent is lost.
func TestQueryKeepsNativeHistogramSamples(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"resultType":"vector","result":[
		{"metric":{"__name__":"latency"},"histogram":[1758326400,{"count":"5","sum":"1.2"}]}]}`))
	got, err := client.Query(context.Background(), InstantRequest{Query: "latency"})
	if err != nil {
		t.Fatal(err)
	}
	sample := got.Series[0].Value
	if sample == nil || len(sample.Histogram) == 0 {
		t.Fatalf("histogram dropped: %+v", sample)
	}
	if !strings.Contains(string(sample.Histogram), `"count":"5"`) {
		t.Fatalf("histogram body altered: %s", sample.Histogram)
	}
}

// Values arrive as strings so NaN and +Inf — which JSON numbers cannot express
// — survive intact.
func TestQueryPreservesSpecialValues(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"resultType":"vector","result":[
		{"metric":{},"value":[1,"NaN"]},{"metric":{},"value":[1,"+Inf"]}]}`))
	got, err := client.Query(context.Background(), InstantRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Series[0].Value.Value != "NaN" || got.Series[1].Value.Value != "+Inf" {
		t.Fatalf("special values altered: %+v", got.Series)
	}
}

// A long expression must not be smuggled into a URL where a proxy can truncate
// it, so every query endpoint is called over POST.
func TestQueriesAreSentAsFormPosts(t *testing.T) {
	t.Parallel()
	var method, query string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		_ = r.ParseForm()
		query = r.FormValue("query")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	})
	long := "sum by (job) (rate(http_requests_total{status=~\"5..\"}[5m]))"
	if _, err := client.Query(context.Background(), InstantRequest{Query: long}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || query != long {
		t.Fatalf("method=%s query=%q", method, query)
	}
}

// A PromQL mistake is the caller's, not the server's: it must classify as a
// usage error so the exit code says "fix the query", not "retry later".
func TestPromQLErrorsClassifyAsUsage(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"parse error: unknown function"}`))
	})
	_, err := client.Query(context.Background(), InstantRequest{Query: "nope("})
	ce := cerrors.AsCLIError(err)
	if ce.Category != cerrors.CategoryUsage || cerrors.ExitCode(ce) != cerrors.ExitUsage {
		t.Fatalf("category=%q exit=%d", ce.Category, cerrors.ExitCode(ce))
	}
	if !strings.Contains(ce.Message, "unknown function") {
		t.Fatalf("server detail lost: %q", ce.Message)
	}
}

// A 422 means the expression parsed but could not run — usually too many
// series — so the guidance must be about narrowing, not about syntax.
func TestExecutionErrorsExplainHowToNarrow(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"execution","error":"query processing would load too many samples"}`))
	})
	_, err := client.Query(context.Background(), InstantRequest{Query: "{__name__=~\".+\"}"})
	ce := cerrors.AsCLIError(err)
	if ce.Code != "QUERY_EXECUTION" || ce.Category != cerrors.CategoryUsage {
		t.Fatalf("got %+v", ce)
	}
	if len(ce.NextSteps) == 0 || !strings.Contains(strings.Join(ce.NextSteps, " "), "Narrow") {
		t.Fatalf("next steps = %v", ce.NextSteps)
	}
}

// A 404 on an admin path means the server was started without
// --web.enable-admin-api, which is a permission problem the caller can act on,
// not a missing resource.
func TestAdminNotFoundBecomesAdminApiDisabled(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"not_found","error":"disabled"}`))
	})
	err := client.CleanTombstones(context.Background())
	ce := cerrors.AsCLIError(err)
	if ce.Code != "ADMIN_API_DISABLED" || cerrors.ExitCode(ce) != cerrors.ExitPermission {
		t.Fatalf("got code=%q exit=%d", ce.Code, cerrors.ExitCode(ce))
	}
	if !strings.Contains(ce.Hint, "--web.enable-admin-api") {
		t.Fatalf("hint = %q", ce.Hint)
	}
}

// A 200 that is not a Prometheus envelope means the URL points somewhere else.
// Treating its empty body as an empty result would be a confident wrong answer.
func TestNonPrometheusResponseIsRejected(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	_, err := client.BuildInfo(context.Background())
	ce := cerrors.AsCLIError(err)
	if ce.Code != "NOT_PROMETHEUS_API" {
		t.Fatalf("got %+v", ce)
	}
}

// The admin endpoints answer 204 with no body; that is success, not a decode
// failure.
func TestNoContentIsSuccess(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if err := client.DeleteSeries(context.Background(), DeleteSeriesRequest{Match: []string{"up"}}); err != nil {
		t.Fatal(err)
	}
}

// Deleting without a selector would match every series, so it is refused before
// a request is built.
func TestDeleteSeriesRequiresASelector(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{}`))
	err := client.DeleteSeries(context.Background(), DeleteSeriesRequest{})
	if ce := cerrors.AsCLIError(err); ce.Code != "NO_MATCH" {
		t.Fatalf("got %+v", ce)
	}
}

func TestReadOnlyBlocksEveryWrite(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{}`))
	ro := NewReadOnly(client)
	ctx := context.Background()

	checks := []struct {
		name string
		err  error
	}{
		{"delete", ro.DeleteSeries(ctx, DeleteSeriesRequest{Match: []string{"up"}})},
		{"clean", ro.CleanTombstones(ctx)},
	}
	if _, err := ro.Snapshot(ctx, false); true {
		checks = append(checks, struct {
			name string
			err  error
		}{"snapshot", err})
	}
	for _, c := range checks {
		ce := cerrors.AsCLIError(c.err)
		if ce.Code != "READONLY_BLOCKED" || cerrors.ExitCode(ce) != cerrors.ExitPermission {
			t.Errorf("%s: got %+v", c.name, ce)
		}
	}
	// Reads must still pass straight through the wrapper.
	if _, err := ro.Status(ctx, "flags", nil); err != nil {
		t.Fatalf("read blocked by the read-only wrapper: %v", err)
	}
}

// Pasting the API endpoint instead of the server root is the most common setup
// mistake; trimming it turns a guaranteed 404 into a working client.
func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"localhost:9090", "http://localhost:9090"},
		{"http://localhost:9090/", "http://localhost:9090"},
		{"https://prom.example.com/api/v1", "https://prom.example.com"},
		{"https://prom.example.com/prom/api/v1/", "https://prom.example.com/prom"},
	} {
		got, err := NormalizeBaseURL(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := NormalizeBaseURL(""); err == nil {
		t.Error("accepted an empty base URL")
	}
}

// The label name is a path segment; one containing a slash must not split the
// route.
func TestLabelValuesEscapesTheLabelName(t *testing.T) {
	t.Parallel()
	var path string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"status":"success","data":["a"]}`))
	})
	if _, err := client.LabelValues(context.Background(), "odd/name", LabelsRequest{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "odd%2Fname") {
		t.Fatalf("label name not escaped: %q", path)
	}
}

// Rules come back nested inside groups; flattening them is what makes a listing
// filterable, and the group's file and interval must survive the flattening.
func TestRulesAreFlattenedWithTheirGroupContext(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"groups":[{"name":"g","file":"/rules.yml","interval":60,
		"rules":[{"type":"alerting","name":"A","state":"firing","health":"ok",
			"alerts":[{"labels":{"alertname":"A"},"state":"firing","activeAt":"2026-09-20T10:00:00Z"}]},
			{"type":"recording","name":"R","health":"err","lastError":"boom"}]}],
		"groupNextToken":"tok"}`))
	page, err := client.Rules(context.Background(), RulesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rules) != 2 || page.NextToken != "tok" {
		t.Fatalf("got %+v", page)
	}
	for _, r := range page.Rules {
		if r.Group != "g" || r.File != "/rules.yml" || r.GroupInterval != 60 {
			t.Fatalf("group context lost: %+v", r)
		}
	}
	if len(page.Rules[0].Alerts) != 1 || page.Rules[0].Alerts[0].ActiveFor == "" {
		t.Fatalf("alert not normalized: %+v", page.Rules[0].Alerts)
	}
}

// Active and dropped targets arrive in two arrays; one flat list with an
// explicit state is what makes "what is broken" a single filter.
func TestTargetsAreFlattenedWithAnExplicitState(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"activeTargets":[
			{"health":"down","labels":{"job":"node","instance":"n1"},"lastError":"refused",
			 "lastScrape":"2026-09-20T10:00:00Z"}],
		"droppedTargets":[{"discoveredLabels":{"__address__":"n2","job":"old"}}]}`))
	targets, err := client.Targets(context.Background(), TargetsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].State != "active" || targets[1].State != "dropped" {
		t.Fatalf("got %+v", targets)
	}
	if targets[0].LastScrapeAgo == "" {
		t.Fatal("last scrape was not rendered as a relative phrase")
	}
	// A dropped target has no post-relabelling labels, so its identity has to
	// come from what was discovered.
	if targets[1].Instance != "n2" || targets[1].Job != "old" {
		t.Fatalf("dropped target identity lost: %+v", targets[1])
	}
}

// Metadata arrives as a map keyed by metric name; a stable sorted list is what
// makes it comparable between runs.
func TestMetadataIsFlattenedAndSorted(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"zeta":[{"type":"gauge"}],"alpha":[{"type":"counter"},{"type":"gauge"}]}`))
	items, err := client.Metadata(context.Background(), MetadataRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].Metric != "alpha" || items[2].Metric != "zeta" {
		t.Fatalf("got %+v", items)
	}
}

func TestStatusRejectsAnUnknownTopic(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{}`))
	_, err := client.Status(context.Background(), "nope", url.Values{})
	if ce := cerrors.AsCLIError(err); ce.Code != "UNKNOWN_STATUS_TOPIC" {
		t.Fatalf("got %+v", ce)
	}
}

// A write must be sent at most once: a retried POST could delete twice.
func TestWritesAreNotRetried(t *testing.T) {
	t.Parallel()
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"unavailable","error":"busy"}`))
	}))
	defer srv.Close()
	client, err := BuildClient(BuildParams{BaseURL: srv.URL, MaxRetries: 3, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_ = client.DeleteSeries(context.Background(), DeleteSeriesRequest{Match: []string{"up"}})
	if attempts != 1 {
		t.Fatalf("write was attempted %d times, want 1", attempts)
	}
}

// The preview a --dry-run prints must be the request that would be sent, not a
// separately constructed description of it.
func TestDeleteSeriesPlanMatchesTheRealRequest(t *testing.T) {
	t.Parallel()
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotPath, gotBody = r.URL.Path, string(buf)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	client, err := BuildClient(BuildParams{BaseURL: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	req := DeleteSeriesRequest{Match: []string{`up{job="old"}`}, Start: time.Unix(100, 0), End: time.Unix(200, 0)}
	if err := client.DeleteSeries(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	plan := DeleteSeriesPlan(srv.URL, req)
	if plan.URL != srv.URL+gotPath || plan.Body != gotBody {
		t.Fatalf("plan %s %q != request %s %q", plan.URL, plan.Body, srv.URL+gotPath, gotBody)
	}
	if plan.Effect == "" || !plan.DryRun {
		t.Fatalf("plan is not a usable preview: %+v", plan)
	}
}

// A transport decorator reaches every request, which is how the tenant header
// and any auth header are applied.
func TestDecoratorsReachEveryRequest(t *testing.T) {
	t.Parallel()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Scope-OrgID")
		_, _ = w.Write([]byte(`{"status":"success","data":{}}`))
	}))
	defer srv.Close()
	client, err := BuildClient(BuildParams{
		BaseURL: srv.URL,
		AuthDecorator: func(r *http.Request) {
			r.Header.Set("X-Scope-OrgID", "team-a")
		},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(context.Background(), "flags", nil); err != nil {
		t.Fatal(err)
	}
	if seen != "team-a" {
		t.Fatalf("tenant header = %q", seen)
	}
}

func TestParseQueryPassesTheServerTreeThrough(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"type":"aggregation","op":"sum"}`))
	tree, err := client.ParseQuery(context.Background(), "sum(up)")
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(tree, &decoded); err != nil || decoded["op"] != "sum" {
		t.Fatalf("tree = %s (%v)", tree, err)
	}
}
