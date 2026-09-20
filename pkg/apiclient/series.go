package apiclient

import (
	"context"
	"fmt"
	"net/url"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
)

// Series returns the label sets of the series matching the selectors.
func (c *apiClient) Series(ctx context.Context, req SeriesRequest) ([]SeriesRef, error) {
	if len(req.Match) == 0 {
		return nil, cerrors.New(cerrors.CategoryUsage, "NO_MATCH",
			"at least one --match selector is required").
			WithHint("A selector is a PromQL series selector, e.g. 'up' or '{job=\"node\"}'.").
			WithNextSteps("prometheus-cli labels values __name__ --since 1h")
	}
	form := url.Values{}
	addMatchers(form, req.Match)
	addWindow(form, req.Start, req.End)
	addLimit(form, req.Limit)

	env, err := c.post(ctx, apiPath("/series"), form)
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}
	out := make([]SeriesRef, 0, len(raw))
	for _, m := range raw {
		out = append(out, SeriesRef{Name: m["__name__"], Metric: m})
	}
	return out, nil
}

// LabelNames returns the label names present in the matching series.
func (c *apiClient) LabelNames(ctx context.Context, req LabelsRequest) ([]string, error) {
	form := url.Values{}
	addMatchers(form, req.Match)
	addWindow(form, req.Start, req.End)
	addLimit(form, req.Limit)

	env, err := c.post(ctx, apiPath("/labels"), form)
	if err != nil {
		return nil, err
	}
	var names []string
	if err := decodeInto(env, &names); err != nil {
		return nil, err
	}
	return names, nil
}

// LabelValues returns the values a label takes in the matching series.
//
// The label name is a path segment, so it is escaped here; a name containing a
// slash still resolves rather than splitting the route.
func (c *apiClient) LabelValues(ctx context.Context, name string, req LabelsRequest) ([]string, error) {
	if name == "" {
		return nil, cerrors.New(cerrors.CategoryUsage, "NO_LABEL",
			"a label name is required").
			WithNextSteps("prometheus-cli labels list --since 1h")
	}
	query := url.Values{}
	addMatchers(query, req.Match)
	addWindow(query, req.Start, req.End)
	addLimit(query, req.Limit)

	env, err := c.get(ctx, apiPath("/label/"+url.PathEscape(name)+"/values"), query)
	if err != nil {
		return nil, err
	}
	var values []string
	if err := decodeInto(env, &values); err != nil {
		return nil, err
	}
	return values, nil
}

// addMatchers appends repeated match[] selectors.
func addMatchers(v url.Values, matchers []string) {
	for _, m := range matchers {
		if m != "" {
			v.Add("match[]", m)
		}
	}
}

// addWindow appends the optional start/end bounds. A zero instant is left out
// so the server applies its own default (its full retention).
func addWindow(v url.Values, start, end time.Time) {
	if !start.IsZero() {
		v.Set("start", timeutil.FormatInstant(start))
	}
	if !end.IsZero() {
		v.Set("end", timeutil.FormatInstant(end))
	}
}

// addLimit appends a positive limit; zero means the endpoint's "disabled".
func addLimit(v url.Values, limit int) {
	if limit > 0 {
		v.Set("limit", fmt.Sprint(limit))
	}
}
