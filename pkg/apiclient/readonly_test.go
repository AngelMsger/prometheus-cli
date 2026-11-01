package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// assertCoversMethods fails unless names is exactly the method set of iface,
// so a table test cannot silently skip a method added to the interface.
func assertCoversMethods(t *testing.T, iface reflect.Type, names []string) {
	t.Helper()
	var want []string
	for i := range iface.NumMethod() {
		want = append(want, iface.Method(i).Name)
	}
	got := slices.Sorted(slices.Values(names))
	if !slices.Equal(got, want) {
		t.Fatalf("table covers %v, %s declares %v", got, iface.Name(), want)
	}
}

// TestReadOnlyBlocksEveryWrite drives every Writer method on the wrapper and
// asserts each one returns a READONLY_BLOCKED permission error that names the
// operation, before any HTTP request is sent.
func TestReadOnlyBlocksEveryWrite(t *testing.T) {
	t.Parallel()
	inner, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("read-only client must not send a request (%s %s)", r.Method, r.URL.Path)
	})
	c := NewReadOnly(inner)
	ctx := context.Background()

	cases := []struct {
		name string
		op   string // the operation the error message must name
		fn   func() error
	}{
		{"DeleteSeries", "delete series", func() error {
			return c.DeleteSeries(ctx, DeleteSeriesRequest{Match: []string{"up"}})
		}},
		{"CleanTombstones", "clean tombstones", func() error { return c.CleanTombstones(ctx) }},
		{"Snapshot", "write a TSDB snapshot", func() error { _, err := c.Snapshot(ctx, true); return err }},
	}
	var names []string
	for _, tc := range cases {
		names = append(names, tc.name)
	}
	assertCoversMethods(t, reflect.TypeFor[Writer](), names)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ce := cerrors.AsCLIError(tc.fn())
			if ce == nil {
				t.Fatalf("%s: expected a CLIError, got nil", tc.name)
			}
			if ce.Category != cerrors.CategoryPermission || ce.Code != "READONLY_BLOCKED" {
				t.Errorf("%s: got %s/%s, want permission/READONLY_BLOCKED", tc.name, ce.Category, ce.Code)
			}
			if !strings.Contains(ce.Message, tc.op) {
				t.Errorf("%s: message %q does not name the operation %q", tc.name, ce.Message, tc.op)
			}
			if !strings.Contains(strings.Join(ce.NextSteps, " "), "--allow-writes") {
				t.Errorf("%s: next_steps missing --allow-writes hint: %v", tc.name, ce.NextSteps)
			}
		})
	}
}

// readCall is one call a recordingReader received.
type readCall struct {
	method string
	args   []any
}

// errCannedRead is returned alongside every canned read result, so delegation
// tests also see errors pass through unchanged.
var errCannedRead = errors.New("canned read error")

// recordingReader answers every Reader method with a fixed result and records
// the call. It embeds Client only to satisfy NewReadOnly's parameter type: a
// write that reached it would panic on the nil embedded interface.
type recordingReader struct {
	Client
	calls []readCall
}

func (r *recordingReader) record(method string, args ...any) {
	r.calls = append(r.calls, readCall{method: method, args: args})
}

func (r *recordingReader) BaseURL() string {
	r.record("BaseURL")
	return "BaseURL"
}

func (r *recordingReader) Ping(ctx context.Context) error {
	r.record("Ping", ctx)
	return errCannedRead
}

func (r *recordingReader) BuildInfo(ctx context.Context) (*BuildInfo, error) {
	r.record("BuildInfo", ctx)
	return &BuildInfo{}, errCannedRead
}

func (r *recordingReader) Query(ctx context.Context, req InstantRequest) (*QueryResult, error) {
	r.record("Query", ctx, req)
	return &QueryResult{}, errCannedRead
}

func (r *recordingReader) QueryRange(ctx context.Context, req RangeRequest) (*QueryResult, error) {
	r.record("QueryRange", ctx, req)
	return &QueryResult{}, errCannedRead
}

func (r *recordingReader) QueryExemplars(ctx context.Context, req ExemplarRequest) ([]ExemplarSet, error) {
	r.record("QueryExemplars", ctx, req)
	return []ExemplarSet{{}}, errCannedRead
}

func (r *recordingReader) FormatQuery(ctx context.Context, promql string) (string, error) {
	r.record("FormatQuery", ctx, promql)
	return "FormatQuery", errCannedRead
}

func (r *recordingReader) ParseQuery(ctx context.Context, promql string) (json.RawMessage, error) {
	r.record("ParseQuery", ctx, promql)
	return json.RawMessage(`{"canned":true}`), errCannedRead
}

func (r *recordingReader) Series(ctx context.Context, req SeriesRequest) ([]SeriesRef, error) {
	r.record("Series", ctx, req)
	return []SeriesRef{{}}, errCannedRead
}

func (r *recordingReader) LabelNames(ctx context.Context, req LabelsRequest) ([]string, error) {
	r.record("LabelNames", ctx, req)
	return []string{"label"}, errCannedRead
}

func (r *recordingReader) LabelValues(ctx context.Context, name string, req LabelsRequest) ([]string, error) {
	r.record("LabelValues", ctx, name, req)
	return []string{"label"}, errCannedRead
}

func (r *recordingReader) Metadata(ctx context.Context, req MetadataRequest) ([]MetricMetadata, error) {
	r.record("Metadata", ctx, req)
	return []MetricMetadata{{}}, errCannedRead
}

func (r *recordingReader) TargetMetadata(ctx context.Context, req TargetMetadataRequest) ([]TargetMetadata, error) {
	r.record("TargetMetadata", ctx, req)
	return []TargetMetadata{{}}, errCannedRead
}

func (r *recordingReader) Targets(ctx context.Context, req TargetsRequest) ([]Target, error) {
	r.record("Targets", ctx, req)
	return []Target{{}}, errCannedRead
}

func (r *recordingReader) Rules(ctx context.Context, req RulesRequest) (*RulesPage, error) {
	r.record("Rules", ctx, req)
	return &RulesPage{}, errCannedRead
}

func (r *recordingReader) Alerts(ctx context.Context) ([]Alert, error) {
	r.record("Alerts", ctx)
	return []Alert{{}}, errCannedRead
}

func (r *recordingReader) Alertmanagers(ctx context.Context) ([]Alertmanager, error) {
	r.record("Alertmanagers", ctx)
	return []Alertmanager{{}}, errCannedRead
}

func (r *recordingReader) Status(ctx context.Context, topic string, query url.Values) (json.RawMessage, error) {
	r.record("Status", ctx, topic, query)
	return json.RawMessage(`{"canned":true}`), errCannedRead
}

type delegationKey struct{}

// TestReadOnlyDelegatesEveryRead calls every Reader method through the
// wrapper and asserts the inner client received exactly that one call with the
// same arguments, and that its results and error came back unchanged.
func TestReadOnlyDelegatesEveryRead(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), delegationKey{}, "read-only delegation")
	// argsFor builds fresh arguments on every call, so the expected values are
	// independent of the ones handed to the wrapper: a wrapper that mutated an
	// argument in place before forwarding it would otherwise change the
	// expectation along with it.
	argsFor := func() map[string][]any {
		return map[string][]any{
			"BaseURL":        nil,
			"Ping":           {ctx},
			"BuildInfo":      {ctx},
			"Query":          {ctx, InstantRequest{Query: "query", Limit: 7}},
			"QueryRange":     {ctx, RangeRequest{Query: "query", Limit: 7}},
			"QueryExemplars": {ctx, ExemplarRequest{Query: "query"}},
			"FormatQuery":    {ctx, "promql"},
			"ParseQuery":     {ctx, "promql"},
			"Series":         {ctx, SeriesRequest{Match: []string{"up"}, Limit: 7}},
			"LabelNames":     {ctx, LabelsRequest{Limit: 7}},
			"LabelValues":    {ctx, "name", LabelsRequest{Limit: 7}},
			"Metadata":       {ctx, MetadataRequest{Metric: "metric", Limit: 7, LimitPerMetric: 7}},
			"TargetMetadata": {ctx, TargetMetadataRequest{MatchTarget: "matchtarget", Metric: "metric", Limit: 7}},
			"Targets":        {ctx, TargetsRequest{State: "state", ScrapePool: "scrapepool"}},
			"Rules":          {ctx, RulesRequest{Type: "type", GroupLimit: 7, GroupNextToken: "groupnexttoken"}},
			"Alerts":         {ctx},
			"Alertmanagers":  {ctx},
			"Status":         {ctx, "topic", url.Values{"q": {"v"}}},
		}
	}
	assertCoversMethods(t, reflect.TypeFor[Reader](), slices.Collect(maps.Keys(argsFor())))

	call := func(c Client, method string, args []any) []any {
		in := make([]reflect.Value, len(args))
		for i, a := range args {
			in[i] = reflect.ValueOf(a)
		}
		var out []any
		for _, v := range reflect.ValueOf(c).MethodByName(method).Call(in) {
			out = append(out, v.Interface())
		}
		return out
	}

	for method := range argsFor() {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			inner := &recordingReader{}
			got := call(NewReadOnly(inner), method, argsFor()[method])
			expected := argsFor()[method]
			if want := call(&recordingReader{}, method, expected); !reflect.DeepEqual(got, want) {
				t.Errorf("results = %#v, want the inner client's %#v", got, want)
			}
			want := []readCall{{method: method, args: expected}}
			if !reflect.DeepEqual(inner.calls, want) {
				t.Errorf("inner client received %#v, want %#v", inner.calls, want)
			}
		})
	}
}
