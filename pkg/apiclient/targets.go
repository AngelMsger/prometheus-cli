package apiclient

import (
	"context"
	"net/url"
	"time"

	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
)

// rawTarget is one entry of the targets endpoint, active or dropped.
type rawTarget struct {
	DiscoveredLabels map[string]string `json:"discoveredLabels"`
	Labels           map[string]string `json:"labels"`
	ScrapePool       string            `json:"scrapePool"`
	ScrapeURL        string            `json:"scrapeUrl"`
	GlobalURL        string            `json:"globalUrl"`
	LastError        string            `json:"lastError"`
	LastScrape       string            `json:"lastScrape"`
	LastScrapeDur    float64           `json:"lastScrapeDuration"`
	Health           string            `json:"health"`
	ScrapeInterval   string            `json:"scrapeInterval"`
	ScrapeTimeout    string            `json:"scrapeTimeout"`
}

// Targets returns the scrape targets, active and/or dropped.
//
// Prometheus splits them into two arrays; the CLI flattens both into one list
// carrying an explicit `state`, so "which targets are down" is one listing and
// one filter rather than two shapes.
func (c *apiClient) Targets(ctx context.Context, req TargetsRequest) ([]Target, error) {
	query := url.Values{}
	if req.State != "" {
		query.Set("state", req.State)
	}
	if req.ScrapePool != "" {
		query.Set("scrapePool", req.ScrapePool)
	}

	env, err := c.get(ctx, apiPath("/targets"), query)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Active  []rawTarget `json:"activeTargets"`
		Dropped []rawTarget `json:"droppedTargets"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}

	out := make([]Target, 0, len(raw.Active)+len(raw.Dropped))
	for _, t := range raw.Active {
		out = append(out, normalizeTarget(t, "active"))
	}
	for _, t := range raw.Dropped {
		out = append(out, normalizeTarget(t, "dropped"))
	}
	return out, nil
}

func normalizeTarget(t rawTarget, state string) Target {
	out := Target{
		State:            state,
		Health:           t.Health,
		ScrapePool:       t.ScrapePool,
		ScrapeURL:        t.ScrapeURL,
		GlobalURL:        t.GlobalURL,
		Job:              t.Labels["job"],
		Instance:         t.Labels["instance"],
		Labels:           t.Labels,
		DiscoveredLabels: t.DiscoveredLabels,
		LastError:        t.LastError,
		LastScrape:       t.LastScrape,
		LastScrapeSecs:   t.LastScrapeDur,
		ScrapeInterval:   t.ScrapeInterval,
		ScrapeTimeout:    t.ScrapeTimeout,
	}
	// A dropped target has no labels after relabelling, so fall back to the
	// discovered ones for the identifiers a reader needs.
	if out.Job == "" {
		out.Job = t.DiscoveredLabels["job"]
	}
	if out.Instance == "" {
		out.Instance = t.DiscoveredLabels["__address__"]
	}
	out.LastScrapeAgo = humanSince(t.LastScrape)
	return out
}

// humanSince renders an RFC3339 instant as a compact "how long ago" phrase.
// A target's staleness is the first thing anyone asks about it, and computing
// it from a timestamp is work the CLI should absorb.
func humanSince(rfc3339 string) string {
	if rfc3339 == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, rfc3339)
	if err != nil {
		return ""
	}
	return timeutil.HumanSince(t)
}
