package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
)

// Query evaluates a PromQL expression at a single instant.
//
// The request goes out as a POST form rather than a GET query string: real
// PromQL expressions routinely exceed what proxies accept in a URL, and
// Prometheus supports POST on every query endpoint for exactly that reason.
func (c *apiClient) Query(ctx context.Context, req InstantRequest) (*QueryResult, error) {
	form := url.Values{}
	form.Set("query", req.Query)
	if !req.Time.IsZero() {
		form.Set("time", timeutil.FormatInstant(req.Time))
	}
	addQueryOptions(form, req.Timeout, req.Limit, req.Stats)

	env, err := c.post(ctx, apiPath("/query"), form)
	if err != nil {
		return nil, err
	}
	out, err := normalizeQuery(req.Query, env)
	if err != nil {
		return nil, err
	}
	if !req.Time.IsZero() {
		out.Time = req.Time.UTC().Format(time.RFC3339)
	}
	return out, nil
}

// QueryRange evaluates a PromQL expression across a window at a resolution.
func (c *apiClient) QueryRange(ctx context.Context, req RangeRequest) (*QueryResult, error) {
	if req.Step <= 0 {
		return nil, cerrors.New(cerrors.CategoryUsage, "NO_STEP",
			"a range query needs a positive step").
			WithNextSteps("prometheus-cli query range --query '<promql>' --since 1h")
	}
	form := url.Values{}
	form.Set("query", req.Query)
	form.Set("start", timeutil.FormatInstant(req.Start))
	form.Set("end", timeutil.FormatInstant(req.End))
	form.Set("step", timeutil.FormatDuration(req.Step))
	addQueryOptions(form, req.Timeout, req.Limit, req.Stats)

	env, err := c.post(ctx, apiPath("/query_range"), form)
	if err != nil {
		return nil, err
	}
	out, err := normalizeQuery(req.Query, env)
	if err != nil {
		return nil, err
	}
	out.Start = req.Start.UTC().Format(time.RFC3339)
	out.End = req.End.UTC().Format(time.RFC3339)
	return out, nil
}

// addQueryOptions applies the options shared by the instant and range
// endpoints. Zero values are left out so the server's own defaults apply.
func addQueryOptions(form url.Values, timeout time.Duration, limit int, stats bool) {
	if timeout > 0 {
		form.Set("timeout", timeutil.FormatDuration(timeout))
	}
	if limit > 0 {
		form.Set("limit", fmt.Sprint(limit))
	}
	if stats {
		form.Set("stats", "all")
	}
}

// QueryExemplars returns the exemplars attached to a selector over a window.
func (c *apiClient) QueryExemplars(ctx context.Context, req ExemplarRequest) ([]ExemplarSet, error) {
	form := url.Values{}
	form.Set("query", req.Query)
	form.Set("start", timeutil.FormatInstant(req.Start))
	form.Set("end", timeutil.FormatInstant(req.End))

	env, err := c.post(ctx, apiPath("/query_exemplars"), form)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		SeriesLabels map[string]string `json:"seriesLabels"`
		Exemplars    []struct {
			Labels    map[string]string `json:"labels"`
			Value     json.RawMessage   `json:"value"`
			Timestamp float64           `json:"timestamp"`
		} `json:"exemplars"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}
	out := make([]ExemplarSet, 0, len(raw))
	for _, set := range raw {
		item := ExemplarSet{
			Name:      set.SeriesLabels["__name__"],
			Metric:    set.SeriesLabels,
			Exemplars: make([]Exemplar, 0, len(set.Exemplars)),
		}
		for _, ex := range set.Exemplars {
			item.Exemplars = append(item.Exemplars, Exemplar{
				Labels:    ex.Labels,
				Value:     scalarString(ex.Value),
				Timestamp: ex.Timestamp,
				Time:      timeutil.RenderInstant(ex.Timestamp),
			})
		}
		out = append(out, item)
	}
	return out, nil
}

// FormatQuery returns a PromQL expression pretty-printed by the server, which
// is also the cheapest way to confirm an expression parses.
func (c *apiClient) FormatQuery(ctx context.Context, promql string) (string, error) {
	env, err := c.post(ctx, apiPath("/format_query"), url.Values{"query": {promql}})
	if err != nil {
		return "", err
	}
	var formatted string
	if err := decodeInto(env, &formatted); err != nil {
		return "", err
	}
	return formatted, nil
}

// ParseQuery returns the server's abstract syntax tree for an expression.
func (c *apiClient) ParseQuery(ctx context.Context, promql string) (json.RawMessage, error) {
	env, err := c.post(ctx, apiPath("/parse_query"), url.Values{"query": {promql}})
	if err != nil {
		return nil, err
	}
	return env.Data, nil
}

// normalizeQuery turns the raw {resultType, result} payload into the CLI's
// Series shape. Prometheus encodes every sample as a positional
// `[timestamp, "value"]` tuple and every scalar/string result as a bare tuple
// rather than a list, so this is where those shapes are absorbed once instead
// of in each caller.
func normalizeQuery(promql string, env *envelope) (*QueryResult, error) {
	var payload struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
		Stats      json.RawMessage `json:"stats,omitempty"`
	}
	if err := decodeInto(env, &payload); err != nil {
		return nil, err
	}

	out := &QueryResult{
		Query:      promql,
		ResultType: payload.ResultType,
		Stats:      payload.Stats,
		Warnings:   env.Warnings,
		Infos:      env.Infos,
		Series:     []Series{},
	}

	switch payload.ResultType {
	case "scalar", "string":
		sample, err := decodeSampleTuple(payload.Result)
		if err != nil {
			return nil, err
		}
		if sample != nil {
			out.Series = append(out.Series, Series{Value: sample})
		}
	case "vector", "matrix", "":
		series, err := decodeSeriesList(payload.Result)
		if err != nil {
			return nil, err
		}
		out.Series = series
	default:
		// An unknown result type is still worth returning rather than failing:
		// keep the payload so the caller sees what the server sent.
		out.Series = []Series{}
		out.Stats = payload.Stats
	}
	out.SeriesCount = len(out.Series)
	return out, nil
}

// decodeSeriesList normalizes a vector or matrix result.
func decodeSeriesList(raw json.RawMessage) ([]Series, error) {
	if len(raw) == 0 {
		return []Series{}, nil
	}
	var entries []struct {
		Metric     map[string]string `json:"metric"`
		Value      json.RawMessage   `json:"value"`
		Values     json.RawMessage   `json:"values"`
		Histogram  json.RawMessage   `json:"histogram"`
		Histograms json.RawMessage   `json:"histograms"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, decodeError(raw, err)
	}
	out := make([]Series, 0, len(entries))
	for _, e := range entries {
		s := Series{Metric: e.Metric, Name: e.Metric["__name__"]}
		switch {
		case len(e.Value) > 0:
			sample, err := decodeSampleTuple(e.Value)
			if err != nil {
				return nil, err
			}
			s.Value = sample
		case len(e.Histogram) > 0:
			sample, err := decodeHistogramTuple(e.Histogram)
			if err != nil {
				return nil, err
			}
			s.Value = sample
		}
		if len(e.Values) > 0 {
			samples, err := decodeSampleTuples(e.Values, decodeSampleTuple)
			if err != nil {
				return nil, err
			}
			s.Values = samples
		}
		if len(e.Histograms) > 0 {
			samples, err := decodeSampleTuples(e.Histograms, decodeHistogramTuple)
			if err != nil {
				return nil, err
			}
			s.Values = append(s.Values, samples...)
		}
		out = append(out, s)
	}
	return out, nil
}

// decodeSampleTuples applies a tuple decoder across a JSON array of tuples.
func decodeSampleTuples(raw json.RawMessage, decode func(json.RawMessage) (*Sample, error)) ([]Sample, error) {
	var tuples []json.RawMessage
	if err := json.Unmarshal(raw, &tuples); err != nil {
		return nil, decodeError(raw, err)
	}
	out := make([]Sample, 0, len(tuples))
	for _, t := range tuples {
		sample, err := decode(t)
		if err != nil {
			return nil, err
		}
		if sample != nil {
			out = append(out, *sample)
		}
	}
	return out, nil
}

// decodeSampleTuple decodes a `[timestamp, "value"]` pair.
func decodeSampleTuple(raw json.RawMessage) (*Sample, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var pair []json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) < 2 {
		return nil, decodeError(raw, fmt.Errorf("expected a [timestamp, value] pair"))
	}
	var ts float64
	if err := json.Unmarshal(pair[0], &ts); err != nil {
		return nil, decodeError(raw, err)
	}
	return &Sample{
		Timestamp: ts,
		Time:      timeutil.RenderInstant(ts),
		Value:     scalarString(pair[1]),
	}, nil
}

// decodeHistogramTuple decodes a `[timestamp, {histogram}]` pair, keeping the
// histogram body verbatim so nothing the server sent is dropped.
func decodeHistogramTuple(raw json.RawMessage) (*Sample, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var pair []json.RawMessage
	if err := json.Unmarshal(raw, &pair); err != nil || len(pair) < 2 {
		return nil, decodeError(raw, fmt.Errorf("expected a [timestamp, histogram] pair"))
	}
	var ts float64
	if err := json.Unmarshal(pair[0], &ts); err != nil {
		return nil, decodeError(raw, err)
	}
	return &Sample{
		Timestamp: ts,
		Time:      timeutil.RenderInstant(ts),
		Histogram: pair[1],
	}, nil
}

// scalarString renders a JSON scalar as the plain string Prometheus wrote.
// Sample values arrive quoted ("42", "NaN"); anything else is passed through
// as written so no precision is lost re-encoding it.
func scalarString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
