package apiclient

import (
	"context"
	"fmt"
	"net/url"
	"sort"
)

// Metadata returns metric metadata (type, help, unit) known to the server.
//
// Prometheus returns this as an object keyed by metric name whose values are
// lists; the CLI flattens it into one row per (metric, entry) and sorts by
// metric name, so the result is a stable, filterable list like every other
// listing in the family rather than a map an agent has to walk.
func (c *apiClient) Metadata(ctx context.Context, req MetadataRequest) ([]MetricMetadata, error) {
	query := url.Values{}
	if req.Metric != "" {
		query.Set("metric", req.Metric)
	}
	if req.Limit > 0 {
		query.Set("limit", fmt.Sprint(req.Limit))
	}
	if req.LimitPerMetric > 0 {
		query.Set("limit_per_metric", fmt.Sprint(req.LimitPerMetric))
	}

	env, err := c.get(ctx, apiPath("/metadata"), query)
	if err != nil {
		return nil, err
	}
	var raw map[string][]struct {
		Type string `json:"type"`
		Help string `json:"help"`
		Unit string `json:"unit"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]MetricMetadata, 0, len(raw))
	for _, name := range names {
		for _, entry := range raw[name] {
			out = append(out, MetricMetadata{
				Metric: name, Type: entry.Type, Help: entry.Help, Unit: entry.Unit,
			})
		}
	}
	return out, nil
}

// TargetMetadata returns metric metadata as scrape targets report it.
func (c *apiClient) TargetMetadata(ctx context.Context, req TargetMetadataRequest) ([]TargetMetadata, error) {
	query := url.Values{}
	if req.MatchTarget != "" {
		query.Set("match_target", req.MatchTarget)
	}
	if req.Metric != "" {
		query.Set("metric", req.Metric)
	}
	if req.Limit > 0 {
		query.Set("limit", fmt.Sprint(req.Limit))
	}

	env, err := c.get(ctx, apiPath("/targets/metadata"), query)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Target map[string]string `json:"target"`
		Metric string            `json:"metric"`
		Type   string            `json:"type"`
		Help   string            `json:"help"`
		Unit   string            `json:"unit"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}
	out := make([]TargetMetadata, 0, len(raw))
	for _, e := range raw {
		out = append(out, TargetMetadata{
			Metric: e.Metric, Type: e.Type, Help: e.Help, Unit: e.Unit, Target: e.Target,
		})
	}
	return out, nil
}
