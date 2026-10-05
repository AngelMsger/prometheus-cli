package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

func TestEmptyResponsesRequireTheCorrectAdminOperation(t *testing.T) {
	t.Parallel()
	operations := map[string]func(Client) error{
		"query":     func(c Client) error { _, err := c.Query(context.Background(), InstantRequest{Query: "up"}); return err },
		"buildinfo": func(c Client) error { _, err := c.BuildInfo(context.Background()); return err },
		"snapshot":  func(c Client) error { _, err := c.Snapshot(context.Background(), false); return err },
		"delete": func(c Client) error {
			return c.DeleteSeries(context.Background(), DeleteSeriesRequest{Match: []string{"up"}})
		},
		"clean": func(c Client) error { return c.CleanTombstones(context.Background()) },
	}
	for name, operation := range operations {
		for _, status := range []int{http.StatusOK, http.StatusNoContent} {
			t.Run(name+http.StatusText(status), func(t *testing.T) {
				client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
				err := operation(client)
				if status == http.StatusNoContent && (name == "delete" || name == "clean") {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if ce := cerrors.AsCLIError(err); ce.Code != "NOT_PROMETHEUS_API" || cerrors.ExitCode(ce) != 10 {
					t.Fatalf("expected a parse error, got %v", err)
				}
			})
		}
	}
}

func TestNonQueryAdvisoriesSurviveThePublicClient(t *testing.T) {
	t.Parallel()
	var got []Advisory
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":["job"],"warnings":["results truncated due to limit"],"infos":["partial index"]}`))
	}))
	defer srv.Close()
	client, err := BuildClient(BuildParams{BaseURL: srv.URL, OnAdvisory: func(a Advisory) { got = append(got, a) }})
	if err != nil {
		t.Fatal(err)
	}
	items, err := NewReadOnly(client).LabelNames(context.Background(), LabelsRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items, []string{"job"}) || len(got) != 1 || got[0].Endpoint != "/api/v1/labels" || len(got[0].Warnings) != 1 || len(got[0].Infos) != 1 {
		t.Fatalf("lost data or advisory: %v, %+v", items, got)
	}
}

func TestInvalidBoundsNeverSendRequests(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		serveData(`{}`)(w, nil)
	})
	ctx := context.Background()
	cases := map[string]func() error{
		"instant limit":   func() error { _, e := client.Query(ctx, InstantRequest{Query: "up", Limit: -1}); return e },
		"instant timeout": func() error { _, e := client.Query(ctx, InstantRequest{Query: "up", Timeout: -time.Second}); return e },
		"range limit": func() error {
			_, e := client.QueryRange(ctx, RangeRequest{Query: "up", Step: time.Second, Limit: -1})
			return e
		},
		"range timeout": func() error {
			_, e := client.QueryRange(ctx, RangeRequest{Query: "up", Step: time.Second, Timeout: -time.Second})
			return e
		},
		"series":              func() error { _, e := client.Series(ctx, SeriesRequest{Match: []string{"up"}, Limit: -1}); return e },
		"labels":              func() error { _, e := client.LabelNames(ctx, LabelsRequest{Limit: -1}); return e },
		"values":              func() error { _, e := client.LabelValues(ctx, "job", LabelsRequest{Limit: -1}); return e },
		"metadata":            func() error { _, e := client.Metadata(ctx, MetadataRequest{Limit: -1}); return e },
		"metadata per metric": func() error { _, e := client.Metadata(ctx, MetadataRequest{LimitPerMetric: -1}); return e },
		"target metadata":     func() error { _, e := client.TargetMetadata(ctx, TargetMetadataRequest{Limit: -1}); return e },
		"rule groups":         func() error { _, e := client.Rules(ctx, RulesRequest{GroupLimit: -1}); return e },
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			err := run()
			if err == nil || cerrors.ExitCode(cerrors.AsCLIError(err)) != 2 {
				t.Fatalf("expected usage error, got %v", err)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("sent %d invalid requests", requests.Load())
	}
}

func TestMixedSamplesAreMergedChronologically(t *testing.T) {
	t.Parallel()
	client, _ := newTestClient(t, serveData(`{"resultType":"matrix","result":[{"metric":{"__name__":"mixed"},"values":[[1000,"1"],[1002,"2"]],"histograms":[[1001,{"count":"1","sum":"1"}]]}]}`))
	result, err := client.QueryRange(context.Background(), RangeRequest{Query: "mixed", Start: time.Unix(1000, 0), End: time.Unix(1002, 0), Step: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	samples := result.Series[0].Values
	if len(samples) != 3 {
		t.Fatalf("samples = %+v", samples)
	}
	for i, sample := range samples {
		if sample.Timestamp != float64(1000+i) {
			t.Fatalf("out of order: %+v", samples)
		}
	}
	if len(samples[1].Histogram) == 0 || samples[0].Value != "1" || samples[2].Value != "2" {
		t.Fatalf("sample content changed: %+v", samples)
	}
}

func TestSampleJSONPreservesEmptyValuesWithoutInventingHistogramValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "0", "NaN", "+Inf", "-Inf", "0.1234567890123456789"} {
		raw, err := json.Marshal(Sample{Timestamp: 1, Time: "1970-01-01T00:00:01Z", Value: value})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got["value"] != value || got["timestamp"] != float64(1) || got["time"] == nil {
			t.Fatalf("sample changed: %s", raw)
		}
	}
	raw, err := json.Marshal(Sample{Histogram: json.RawMessage(`{"count":"1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := got["value"]; exists || got["histogram"] == nil {
		t.Fatalf("histogram changed: %s", raw)
	}
}
