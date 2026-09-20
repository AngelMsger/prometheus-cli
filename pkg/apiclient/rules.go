package apiclient

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
)

// Rules returns the configured recording and alerting rules.
//
// Prometheus returns rules nested inside groups, with the group's file and
// interval only available on the parent. The CLI flattens them into one row
// per rule carrying `group`, `file` and `group_interval_seconds`, so a caller
// can filter and report on rules directly — and still page by group through
// the server's own group cursor.
func (c *apiClient) Rules(ctx context.Context, req RulesRequest) (*RulesPage, error) {
	query := url.Values{}
	if req.Type != "" {
		query.Set("type", req.Type)
	}
	for _, n := range req.RuleName {
		query.Add("rule_name[]", n)
	}
	for _, g := range req.RuleGroup {
		query.Add("rule_group[]", g)
	}
	for _, f := range req.File {
		query.Add("file[]", f)
	}
	addMatchers(query, req.Match)
	if req.ExcludeAlerts {
		query.Set("exclude_alerts", "true")
	}
	if req.GroupLimit > 0 {
		query.Set("group_limit", fmt.Sprint(req.GroupLimit))
	}
	if req.GroupNextToken != "" {
		query.Set("group_next_token", req.GroupNextToken)
	}

	env, err := c.get(ctx, apiPath("/rules"), query)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Groups []struct {
			Name     string  `json:"name"`
			File     string  `json:"file"`
			Interval float64 `json:"interval"`
			Rules    []struct {
				Type           string            `json:"type"`
				Name           string            `json:"name"`
				Query          string            `json:"query"`
				Health         string            `json:"health"`
				LastError      string            `json:"lastError"`
				State          string            `json:"state"`
				Duration       float64           `json:"duration"`
				KeepFiringFor  float64           `json:"keepFiringFor"`
				Labels         map[string]string `json:"labels"`
				Annotations    map[string]string `json:"annotations"`
				EvaluationTime float64           `json:"evaluationTime"`
				LastEvaluation string            `json:"lastEvaluation"`
				Alerts         []rawAlert        `json:"alerts"`
			} `json:"rules"`
		} `json:"groups"`
		NextToken string `json:"groupNextToken"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}

	page := &RulesPage{NextToken: raw.NextToken, Rules: []Rule{}}
	for _, g := range raw.Groups {
		for _, r := range g.Rules {
			rule := Rule{
				Type:           r.Type,
				Name:           r.Name,
				Group:          g.Name,
				File:           g.File,
				Query:          r.Query,
				Health:         r.Health,
				LastError:      r.LastError,
				State:          r.State,
				Duration:       r.Duration,
				KeepFiringFor:  r.KeepFiringFor,
				Labels:         r.Labels,
				Annotations:    r.Annotations,
				EvaluationTime: r.EvaluationTime,
				LastEvaluation: r.LastEvaluation,
				GroupInterval:  g.Interval,
			}
			for _, a := range r.Alerts {
				rule.Alerts = append(rule.Alerts, normalizeAlert(a, r.Name))
			}
			page.Rules = append(page.Rules, rule)
		}
	}
	return page, nil
}

// rawAlert is one alert instance as the rules and alerts endpoints send it.
type rawAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	ActiveAt    string            `json:"activeAt"`
	Value       string            `json:"value"`
}

// Alerts returns the currently active alerts.
func (c *apiClient) Alerts(ctx context.Context) ([]Alert, error) {
	env, err := c.get(ctx, apiPath("/alerts"), nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Alerts []rawAlert `json:"alerts"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}
	out := make([]Alert, 0, len(raw.Alerts))
	for _, a := range raw.Alerts {
		out = append(out, normalizeAlert(a, ""))
	}
	return out, nil
}

// normalizeAlert lifts the alert name out of the label set and renders how long
// the alert has been active — the two things a reader asks for first.
func normalizeAlert(a rawAlert, fallbackName string) Alert {
	name := a.Labels["alertname"]
	if name == "" {
		name = fallbackName
	}
	out := Alert{
		Name:        name,
		State:       a.State,
		ActiveAt:    a.ActiveAt,
		Value:       a.Value,
		Labels:      a.Labels,
		Annotations: a.Annotations,
	}
	if t, err := time.Parse(time.RFC3339Nano, a.ActiveAt); err == nil {
		out.ActiveFor = timeutil.HumanSince(t)
	}
	return out
}

// Alertmanagers returns the Alertmanager endpoints Prometheus knows about,
// flattened into one list with an explicit active/dropped state.
func (c *apiClient) Alertmanagers(ctx context.Context) ([]Alertmanager, error) {
	env, err := c.get(ctx, apiPath("/alertmanagers"), nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Active  []struct{ URL string } `json:"activeAlertmanagers"`
		Dropped []struct{ URL string } `json:"droppedAlertmanagers"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return nil, err
	}
	out := make([]Alertmanager, 0, len(raw.Active)+len(raw.Dropped))
	for _, a := range raw.Active {
		out = append(out, Alertmanager{State: "active", URL: a.URL})
	}
	for _, a := range raw.Dropped {
		out = append(out, Alertmanager{State: "dropped", URL: a.URL})
	}
	return out, nil
}
