package apiclient

import (
	"encoding/json"
	"time"
)

// --- requests -------------------------------------------------------------

// InstantRequest evaluates a PromQL expression at a single instant.
type InstantRequest struct {
	// Query is the PromQL expression. Required.
	Query string
	// Time is the evaluation instant. The zero value means "now", which is what
	// Prometheus defaults to when the parameter is omitted.
	Time time.Time
	// Timeout bounds evaluation server-side. Zero leaves the server default.
	Timeout time.Duration
	// Limit caps the number of returned series. Zero means unlimited.
	Limit int
	// Stats requests query statistics alongside the result.
	Stats bool
}

// RangeRequest evaluates a PromQL expression across a window at a resolution.
type RangeRequest struct {
	Query   string
	Start   time.Time
	End     time.Time
	Step    time.Duration
	Timeout time.Duration
	Limit   int
	Stats   bool
}

// ExemplarRequest selects the exemplars attached to a series selector.
type ExemplarRequest struct {
	Query string
	Start time.Time
	End   time.Time
}

// SeriesRequest selects series by label matchers over an optional window.
type SeriesRequest struct {
	// Match holds one or more series selectors, e.g. `up{job="node"}`. At least
	// one is required by the endpoint.
	Match []string
	Start time.Time
	End   time.Time
	Limit int
}

// LabelsRequest is the shared shape of the label-name and label-value lookups.
type LabelsRequest struct {
	Match []string
	Start time.Time
	End   time.Time
	Limit int
}

// MetadataRequest selects metric metadata held by the server.
type MetadataRequest struct {
	// Metric restricts the result to one metric name. Empty returns all.
	Metric string
	// Limit caps the number of metric names returned. Zero means unlimited.
	Limit int
	// LimitPerMetric caps the entries returned per metric name.
	LimitPerMetric int
}

// TargetMetadataRequest selects metric metadata as scrape targets report it.
type TargetMetadataRequest struct {
	// MatchTarget is a label selector over target labels, e.g. `{job="node"}`.
	MatchTarget string
	// Metric restricts the result to one metric name.
	Metric string
	Limit  int
}

// TargetsRequest selects scrape targets.
type TargetsRequest struct {
	// State is "active", "dropped" or "any" (the default when empty is the
	// server's, which returns both).
	State string
	// ScrapePool restricts the result to one scrape pool.
	ScrapePool string
}

// RulesRequest selects rule groups.
type RulesRequest struct {
	// Type is "alert" or "record"; empty returns both.
	Type string
	// RuleName, RuleGroup and File filter by exact name.
	RuleName  []string
	RuleGroup []string
	File      []string
	// Match holds label selectors applied to alerting rules' alerts.
	Match []string
	// ExcludeAlerts drops the per-rule active alerts, which is much cheaper on
	// an instance with many firing alerts.
	ExcludeAlerts bool
	// GroupLimit pages the result by rule group; GroupNextToken resumes.
	GroupLimit     int
	GroupNextToken string
}

// DeleteSeriesRequest selects the series whose samples are to be deleted.
type DeleteSeriesRequest struct {
	Match []string
	Start time.Time
	End   time.Time
}

// --- results --------------------------------------------------------------

// Sample is one evaluated point. Prometheus sends these as a
// `[1700000000.123, "42"]` tuple; the CLI normalizes the tuple into named
// fields and adds the RFC3339 rendering, so no caller has to decode a
// positional array or convert a fractional epoch by hand.
type Sample struct {
	// Timestamp is the sample instant in fractional Unix seconds, as sent.
	Timestamp float64 `json:"timestamp"`
	// Time is the same instant rendered as RFC3339 in UTC.
	Time string `json:"time"`
	// Value is the sample value exactly as Prometheus wrote it, including
	// "NaN", "+Inf" and full float precision. It is kept as a string on
	// purpose: JSON numbers cannot carry those, and re-encoding a float loses
	// digits the server sent.
	Value string `json:"value,omitempty"`
	// Histogram carries a native-histogram sample verbatim, for the series
	// where Prometheus sends a histogram instead of a float value.
	Histogram json.RawMessage `json:"histogram,omitempty"`
}

// Series is one result series: its label set plus either a single sample
// (instant queries) or a list of samples (range queries).
type Series struct {
	// Name is the series' __name__ label, lifted out for readability.
	Name string `json:"name,omitempty"`
	// Metric is the full label set, including __name__.
	Metric map[string]string `json:"metric,omitempty"`
	// Value is the single sample of a vector, scalar or string result.
	Value *Sample `json:"value,omitempty"`
	// Values are the samples of a matrix result, oldest first.
	Values []Sample `json:"values,omitempty"`
}

// QueryResult is a normalized PromQL reply.
type QueryResult struct {
	// Query is the expression that was evaluated, echoed back so a result can
	// be interpreted without the command line that produced it.
	Query string `json:"query"`
	// ResultType is Prometheus' own result type: vector, matrix, scalar or
	// string. It tells the caller whether to read Value or Values.
	ResultType string `json:"result_type"`
	// Step is the resolution a range query ran at, and how it was chosen.
	// Nil for instant queries.
	Step *StepInfo `json:"step,omitempty"`
	// Start and End bound a range query, in RFC3339. Empty for instant queries.
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	// Time is the evaluation instant of an instant query, in RFC3339.
	Time string `json:"time,omitempty"`
	// SeriesCount is len(Series), surfaced so `--fields` projections and table
	// output can report the size without re-counting.
	SeriesCount int `json:"series_count"`
	// Series holds the normalized result.
	Series []Series `json:"series"`
	// Stats carries the server's query statistics when they were requested.
	Stats json.RawMessage `json:"stats,omitempty"`
	// Warnings and Infos are the server's own advisories about this evaluation
	// (for example a partial response). They are part of the answer, not
	// diagnostics, so they travel with the result on stdout.
	Warnings []string `json:"warnings,omitempty"`
	Infos    []string `json:"infos,omitempty"`
}

// StepInfo reports the resolution of a range query and where it came from.
type StepInfo struct {
	// Value is the resolution in Prometheus duration form, e.g. "1m".
	Value string `json:"value"`
	// Source is "flag" (the caller passed --step), "derived" (the CLI chose a
	// resolution for the window) or "clamped" (the caller's step would have
	// exceeded Prometheus' 11,000-point ceiling and was raised).
	Source string `json:"source"`
	// Points is how many samples per series the window resolves to.
	Points int `json:"points"`
}

// Exemplar is one trace exemplar attached to a sample.
type Exemplar struct {
	Labels    map[string]string `json:"labels,omitempty"`
	Value     string            `json:"value,omitempty"`
	Timestamp float64           `json:"timestamp"`
	Time      string            `json:"time,omitempty"`
}

// ExemplarSet groups the exemplars belonging to one series.
type ExemplarSet struct {
	Name      string            `json:"name,omitempty"`
	Metric    map[string]string `json:"metric,omitempty"`
	Exemplars []Exemplar        `json:"exemplars"`
}

// SeriesRef is one series identified by its label set.
type SeriesRef struct {
	Name   string            `json:"name,omitempty"`
	Metric map[string]string `json:"metric"`
}

// MetricMetadata is the type/help/unit a server knows for a metric name.
type MetricMetadata struct {
	Metric string `json:"metric"`
	Type   string `json:"type,omitempty"`
	Help   string `json:"help,omitempty"`
	Unit   string `json:"unit,omitempty"`
}

// TargetMetadata is metric metadata as one scrape target reports it.
type TargetMetadata struct {
	Metric string            `json:"metric,omitempty"`
	Type   string            `json:"type,omitempty"`
	Help   string            `json:"help,omitempty"`
	Unit   string            `json:"unit,omitempty"`
	Target map[string]string `json:"target,omitempty"`
}

// Target is one scrape target, active or dropped.
type Target struct {
	// State is "active" or "dropped", lifted from which list it came back in
	// so a single flat listing stays unambiguous.
	State string `json:"state"`
	// Health is Prometheus' own up/down/unknown verdict for an active target.
	Health     string            `json:"health,omitempty"`
	ScrapePool string            `json:"scrape_pool,omitempty"`
	ScrapeURL  string            `json:"scrape_url,omitempty"`
	GlobalURL  string            `json:"global_url,omitempty"`
	Job        string            `json:"job,omitempty"`
	Instance   string            `json:"instance,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	// DiscoveredLabels are the pre-relabelling labels — the only way to see why
	// a dropped target was dropped.
	DiscoveredLabels map[string]string `json:"discovered_labels,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	LastScrape       string            `json:"last_scrape,omitempty"`
	LastScrapeAgo    string            `json:"last_scrape_ago,omitempty"`
	LastScrapeSecs   float64           `json:"last_scrape_duration_seconds,omitempty"`
	ScrapeInterval   string            `json:"scrape_interval,omitempty"`
	ScrapeTimeout    string            `json:"scrape_timeout,omitempty"`
}

// Rule is one recording or alerting rule, flattened out of its group so a
// listing can be filtered and read without walking a nested structure.
type Rule struct {
	// Type is "recording" or "alerting".
	Type string `json:"type"`
	Name string `json:"name"`
	// Group and File say where the rule is defined — the identifiers the
	// --rule-group and --file filters take.
	Group string `json:"group,omitempty"`
	File  string `json:"file,omitempty"`
	Query string `json:"query,omitempty"`
	// Health is "ok", "err" or "unknown"; LastError explains a failing rule.
	Health    string `json:"health,omitempty"`
	LastError string `json:"last_error,omitempty"`
	// State is an alerting rule's inactive/pending/firing status.
	State string `json:"state,omitempty"`
	// Duration is an alerting rule's `for` clause, in seconds.
	Duration       float64           `json:"duration,omitempty"`
	KeepFiringFor  float64           `json:"keep_firing_for,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
	Alerts         []Alert           `json:"alerts,omitempty"`
	EvaluationTime float64           `json:"evaluation_time_seconds,omitempty"`
	LastEvaluation string            `json:"last_evaluation,omitempty"`
	// GroupInterval is the group's evaluation interval, in seconds.
	GroupInterval float64 `json:"group_interval_seconds,omitempty"`
}

// RulesPage is one page of flattened rules plus the group cursor.
type RulesPage struct {
	Rules []Rule
	// NextToken resumes a --group-limit paged listing; empty when complete.
	NextToken string
}

// Alert is one active alert instance.
type Alert struct {
	Name        string            `json:"name,omitempty"`
	State       string            `json:"state,omitempty"`
	ActiveAt    string            `json:"active_at,omitempty"`
	ActiveFor   string            `json:"active_for,omitempty"`
	Value       string            `json:"value,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Alertmanager is one Alertmanager endpoint Prometheus knows about.
type Alertmanager struct {
	// State is "active" or "dropped".
	State string `json:"state"`
	URL   string `json:"url"`
}

// BuildInfo is the server's version and build metadata.
type BuildInfo struct {
	Version   string `json:"version,omitempty"`
	Revision  string `json:"revision,omitempty"`
	Branch    string `json:"branch,omitempty"`
	BuildUser string `json:"buildUser,omitempty"`
	BuildDate string `json:"buildDate,omitempty"`
	GoVersion string `json:"goVersion,omitempty"`
}

// WritePlan is the credentials-free preview a --dry-run write emits: exactly
// the request that would have been sent, and nothing is sent.
type WritePlan struct {
	DryRun bool   `json:"dry_run"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Body   string `json:"body,omitempty"`
	// Effect states in plain words what executing the plan would change, so a
	// preview can be read without knowing the endpoint.
	Effect string `json:"effect,omitempty"`
}
