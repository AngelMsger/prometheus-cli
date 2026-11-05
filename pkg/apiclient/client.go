// Package apiclient is the Prometheus HTTP API surface used by the CLI. It
// builds requests against the /api/v1 endpoints, decodes the standard
// {status,data,errorType,error,warnings} envelope into normalized models, and
// converts failures into structured *errors.CLIError values.
//
// This package backs the prometheus-cli command layer and is also importable as
// a standalone client library (e.g. by a GUI); see the repository README. Its
// exported surface — the Client interface, the normalized models, and the
// read-only / dry-run semantics — is a contract the CLI and its companion Skill
// depend on. Extend it additively and keep existing shapes and behavior stable;
// do not reshape the public API to suit a single local call site.
package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/transport"
)

// Client is the Prometheus API surface used by the CLI. It is the union of
// Reader and Writer, so every method is classified as one or the other;
// NewReadOnly relies on that split.
type Client interface {
	Reader
	Writer
}

// Reader is the part of Client that never changes server state: queries
// (including the ones sent as POST form bodies), discovery, targets, rules,
// alerts, status and connectivity probes. The read-only wrapper delegates
// exactly this interface, so a method that can mutate Prometheus must never be
// declared here.
type Reader interface {
	// BaseURL returns the normalized Prometheus server root (no trailing slash).
	BaseURL() string

	// Ping verifies connectivity and credentials against the build-info endpoint.
	Ping(ctx context.Context) error
	// BuildInfo returns the server's version and build metadata.
	BuildInfo(ctx context.Context) (*BuildInfo, error)

	// Query evaluates a PromQL expression at a single instant.
	Query(ctx context.Context, req InstantRequest) (*QueryResult, error)
	// QueryRange evaluates a PromQL expression across a window at a resolution.
	QueryRange(ctx context.Context, req RangeRequest) (*QueryResult, error)
	// QueryExemplars returns the exemplars attached to a selector over a window.
	QueryExemplars(ctx context.Context, req ExemplarRequest) ([]ExemplarSet, error)
	// FormatQuery returns a PromQL expression pretty-printed by the server.
	FormatQuery(ctx context.Context, promql string) (string, error)
	// ParseQuery returns the server's abstract syntax tree for an expression.
	ParseQuery(ctx context.Context, promql string) (json.RawMessage, error)

	// Series returns the label sets of the series matching the selectors.
	Series(ctx context.Context, req SeriesRequest) ([]SeriesRef, error)
	// LabelNames returns the label names present in the matching series.
	LabelNames(ctx context.Context, req LabelsRequest) ([]string, error)
	// LabelValues returns the values a label takes in the matching series.
	LabelValues(ctx context.Context, name string, req LabelsRequest) ([]string, error)

	// Metadata returns metric metadata (type, help, unit) known to the server.
	Metadata(ctx context.Context, req MetadataRequest) ([]MetricMetadata, error)
	// TargetMetadata returns metric metadata as reported by scrape targets.
	TargetMetadata(ctx context.Context, req TargetMetadataRequest) ([]TargetMetadata, error)

	// Targets returns the scrape targets, active and/or dropped.
	Targets(ctx context.Context, req TargetsRequest) ([]Target, error)

	// Rules returns the configured recording and alerting rules, flattened.
	Rules(ctx context.Context, req RulesRequest) (*RulesPage, error)
	// Alerts returns the currently active alerts.
	Alerts(ctx context.Context) ([]Alert, error)
	// Alertmanagers returns the Alertmanager endpoints Prometheus knows about.
	Alertmanagers(ctx context.Context) ([]Alertmanager, error)

	// Status returns one server status document (config, flags, runtimeinfo,
	// buildinfo, tsdb, walreplay or notifications) as decoded JSON.
	Status(ctx context.Context, topic string, query url.Values) (json.RawMessage, error)
}

// Writer is the part of Client that changes server state: the TSDB admin API.
// The read-only wrapper (NewReadOnly) blocks each method explicitly, so adding
// one here without its blocking override fails compilation.
type Writer interface {
	// DeleteSeries marks the samples matching the selectors as deleted.
	DeleteSeries(ctx context.Context, req DeleteSeriesRequest) error
	// CleanTombstones removes the deleted blocks from disk.
	CleanTombstones(ctx context.Context) error
	// Snapshot writes a TSDB snapshot and returns its directory name.
	Snapshot(ctx context.Context, skipHead bool) (string, error)
}

// apiClient is the single Client implementation.
type apiClient struct {
	baseURL    string // server root, no trailing slash
	http       *transport.Client
	onAdvisory func(Advisory)
}

// Config configures a Client.
type Config struct {
	BaseURL   string
	Transport *transport.Client
	// OnAdvisory receives successful response warnings and infos synchronously.
	// Callers sharing a client concurrently must make the callback thread-safe.
	OnAdvisory func(Advisory)
}

// New builds a Client. The transport must already carry the auth decorator.
func New(cfg Config) Client {
	return &apiClient{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		http:       cfg.Transport,
		onAdvisory: cfg.OnAdvisory,
	}
}

func (c *apiClient) BaseURL() string { return c.baseURL }

// envelope is the standard Prometheus API response wrapper. Every /api/v1
// endpoint returns it, on success and on failure alike.
type envelope struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data,omitempty"`
	ErrorType string          `json:"errorType,omitempty"`
	Error     string          `json:"error,omitempty"`
	Warnings  []string        `json:"warnings,omitempty"`
	Infos     []string        `json:"infos,omitempty"`
}

// apiPath joins the versioned API prefix with an endpoint path.
func apiPath(rest string) string { return constants.APIPrefix + rest }

// get issues a GET against an /api/v1 endpoint and returns the decoded envelope.
func (c *apiClient) get(ctx context.Context, path string, query url.Values) (*envelope, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryUsage, "BAD_REQUEST", "failed to build request")
	}
	req.Header.Set("Accept", "application/json")
	return c.do(ctx, req, path)
}

// postRead posts a query endpoint's form. The request only reads, so the
// transport retries it and follows its redirect as it does a GET
// (transport.AsRead): an http-to-https 307 or 308 in front of Prometheus
// sends the same query again.
func (c *apiClient) postRead(ctx context.Context, path string, form url.Values) (*envelope, error) {
	return c.post(transport.AsRead(ctx), path, form)
}

// post issues a POST with a form body. Prometheus accepts every query endpoint
// over POST, which is how the CLI sends expressions too long for a URL (see
// postRead), and it is the only method the admin endpoints accept.
func (c *apiClient) post(ctx context.Context, path string, form url.Values) (*envelope, error) {
	var body io.Reader
	if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryUsage, "BAD_REQUEST", "failed to build request")
	}
	req.Header.Set("Accept", "application/json")
	if len(form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c.do(ctx, req, path)
}

// do sends a prepared request and turns the response into an envelope or a
// classified error. A 204 (the admin endpoints' success) yields an empty
// envelope rather than a decode failure.
func (c *apiClient) do(ctx context.Context, req *http.Request, path string) (*envelope, error) {
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, cerrors.Wrap(err, cerrors.CategoryNetwork, "NETWORK",
			fmt.Sprintf("request to %s failed", req.URL.Redacted()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// The transport hands back a redirect it does not follow: any redirect of
	// an admin write, and a 301, 302 or 303 of a posted query, which Go would
	// send as a GET without the query. Its body is a login page or nothing, so
	// it is reported as the redirect it is, not as a response that is not
	// Prometheus.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, redirectError(resp, path)
	}
	if resp.StatusCode == http.StatusNoContent || len(strings.TrimSpace(string(raw))) == 0 {
		if resp.StatusCode >= 400 {
			return nil, c.httpError(resp.StatusCode, nil, raw, path)
		}
		if resp.StatusCode == http.StatusNoContent && req.Method == http.MethodPost &&
			(path == apiPath(pathDeleteSeries) || path == apiPath(pathCleanTombstones)) {
			return &envelope{Status: "success"}, nil
		}
		return nil, notPrometheusError(req.URL.Redacted(), raw)
	}

	var env envelope
	if derr := json.Unmarshal(raw, &env); derr != nil {
		if resp.StatusCode >= 400 {
			return nil, c.httpError(resp.StatusCode, nil, raw, path)
		}
		return nil, decodeError(raw, derr)
	}
	if resp.StatusCode >= 400 || env.Status == "error" {
		return nil, c.httpError(resp.StatusCode, &env, raw, path)
	}
	// Every /api/v1 endpoint sets status on success. A 200 without it is not a
	// Prometheus API at all — typically a proxy's login page, a dashboard, or a
	// catch-all route — and silently treating its empty body as an empty result
	// would turn a misconfigured URL into a confident wrong answer.
	if env.Status != "success" {
		return nil, notPrometheusError(req.URL.Redacted(), raw)
	}
	if c.onAdvisory != nil && (len(env.Warnings) > 0 || len(env.Infos) > 0) {
		c.onAdvisory(Advisory{
			Endpoint: path,
			Warnings: append([]string(nil), env.Warnings...),
			Infos:    append([]string(nil), env.Infos...),
		})
	}
	return &env, nil
}

// decodeInto unmarshals an envelope's data payload into out.
func decodeInto(env *envelope, out any) error {
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return decodeError(env.Data, err)
	}
	return nil
}

// notPrometheusError reports a 200 response that does not carry the Prometheus
// API envelope, so a wrong base URL is diagnosed as such instead of surfacing
// as empty data.
func notPrometheusError(endpoint string, body []byte) error {
	snippet := firstLine(string(body))
	return cerrors.Newf(cerrors.CategoryParse, "NOT_PROMETHEUS_API",
		"%s answered without the Prometheus API envelope", endpoint).
		WithHint("The response had no \"status\" field, so this URL is probably not a "+
			"Prometheus HTTP API — check that --base-url is the server root and that no "+
			"proxy is intercepting the request.").
		WithNextSteps(
			"prometheus-cli doctor",
			"prometheus-cli --verbose status buildinfo",
			"Response began: "+snippet)
}

// decodeError reports a response the client could not interpret, keeping a
// snippet so the shape mismatch is diagnosable.
func decodeError(body []byte, cause error) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	return cerrors.Wrap(cause, cerrors.CategoryParse, "DECODE",
		fmt.Sprintf("could not decode the server response: %v", cause)).
		WithHint("The server's JSON did not match what prometheus-cli expected; "+
			"this is likely a client bug, or the URL points at something that is not a Prometheus API.").
		WithNextSteps(
			"prometheus-cli doctor",
			"Retry with --verbose to inspect the request URL.",
			"Report it with this snippet: "+snippet)
}

// httpError turns a failed response into a classified CLIError, preferring the
// Prometheus envelope's own errorType/error over the bare status code.
func (c *apiClient) httpError(status int, env *envelope, raw []byte, path string) error {
	cat := cerrors.FromHTTPStatus(status)
	detail := ""
	errType := ""
	if env != nil {
		detail, errType = env.Error, env.ErrorType
	}
	if detail == "" {
		detail = firstLine(string(raw))
	}

	msg := fmt.Sprintf("Prometheus returned HTTP %d", status)
	if errType != "" {
		msg += " (" + errType + ")"
	}
	if detail != "" {
		msg += ": " + detail
	}

	// bad_data / execution errors are the caller's PromQL or selector, not the
	// server's fault — classify them as usage so the exit code says "fix the
	// query" rather than "retry later".
	switch errType {
	case "bad_data", "execution":
		cat = cerrors.CategoryUsage
	case "timeout", "canceled":
		cat = cerrors.CategoryServer
	case "unavailable":
		cat = cerrors.CategoryServer
	}

	switch {
	case status == http.StatusUnauthorized:
		return cerrors.New(cat, "HTTP_UNAUTHORIZED", msg).
			WithHTTPStatus(status).
			WithHint("The gateway in front of Prometheus rejected the credentials. Prometheus itself "+
				"usually has no authentication, so this is a proxy, tenant or cloud endpoint.").
			WithNextSteps("prometheus-cli auth status", "prometheus-cli auth guide", "prometheus-cli config init")
	case status == http.StatusForbidden:
		return cerrors.New(cat, "HTTP_FORBIDDEN", msg).
			WithHTTPStatus(status).
			WithHint("Authenticated, but this identity is not allowed to call that endpoint "+
				"(a read-only proxy, or a tenant without admin rights).").
			WithNextSteps("prometheus-cli status buildinfo", "prometheus-cli auth status")
	case status == http.StatusNotFound && strings.Contains(path, "/admin/"):
		return cerrors.New(cerrors.CategoryPermission, "ADMIN_API_DISABLED", msg).
			WithHTTPStatus(status).
			WithHint("The TSDB admin API is disabled. Prometheus must be started with "+
				"--web.enable-admin-api for delete-series, clean-tombstones and snapshot.").
			WithNextSteps(
				"prometheus-cli status flags",
				"Restart Prometheus with --web.enable-admin-api, or perform the change on the server.")
	case status == http.StatusNotFound:
		return cerrors.New(cat, "HTTP_NOT_FOUND", msg).
			WithHTTPStatus(status).
			WithHint("No such endpoint at that URL. --base-url must point at the Prometheus "+
				"server root (the part before /api/v1), and the endpoint may not exist in this version.").
			WithNextSteps("prometheus-cli doctor", "prometheus-cli status buildinfo")
	case status == http.StatusUnprocessableEntity:
		return cerrors.New(cat, "QUERY_EXECUTION", msg).
			WithHTTPStatus(status).
			WithHint("Prometheus parsed the expression but could not execute it — commonly too "+
				"many series, or a subquery/step the server refuses.").
			WithNextSteps(
				"Narrow the selector with more label matchers, or aggregate with sum by (...).",
				"Shorten the window, or raise --step.")
	case status == http.StatusBadRequest:
		return cerrors.New(cat, "BAD_QUERY", msg).
			WithHTTPStatus(status).
			WithHint("Prometheus rejected the request — usually a PromQL syntax error, an unknown "+
				"metric name, or a malformed time or duration.").
			WithNextSteps(
				"prometheus-cli query parse --query '<promql>'",
				"prometheus-cli metadata list --metric <name>",
				"prometheus-cli labels values __name__ --since 1h")
	default:
		return cerrors.New(cat, "HTTP_"+statusSlug(status), msg).WithHTTPStatus(status)
	}
}

// redirectError reports a redirect the transport returned instead of
// following: an admin write's, a posted query's 301, 302 or 303, or the tenth
// hop. The kit's redirect rule names where it points and --base-url, and it
// is not retryable, since the same request is redirected again. A proxy can
// pass an admin write on to Prometheus and then redirect, so the redirect
// does not show the write was refused: each write's hint says how to tell
// whether it applied.
func redirectError(resp *http.Response, path string) error {
	status := resp.StatusCode
	loc, _ := resp.Location()
	e := cerrors.New(cerrors.FromHTTPStatus(status), "HTTP_"+statusSlug(status),
		fmt.Sprintf("Prometheus returned HTTP %d", status)).
		WithHTTPStatus(status).
		WithRedirect(loc, redirectGuide)
	switch path {
	case apiPath(pathDeleteSeries):
		// DeleteSeries adds how to repeat it, with its own selectors and
		// window.
		e.Hint += " The delete may still have applied behind the redirect, and its outcome " +
			"stays unknown: Prometheus can keep listing a series whose samples were deleted, " +
			"so a series list does not show it failed. Check the samples in its window and the " +
			"server's logs, and repeat it only if the user confirms."
	case apiPath(pathCleanTombstones):
		e.Hint += " The clean-up may still have applied behind the redirect. Repeating it is " +
			"safe once the URL is fixed: it only erases data already marked deleted."
	case apiPath(pathSnapshot):
		e.Hint += " A snapshot may still have applied behind the redirect: before taking " +
			"another, look for a new directory under snapshots/ in the server's data " +
			"directory (storage.tsdb.path in status flags)."
		e.NextSteps = append(e.NextSteps, "prometheus-cli status flags")
	}
	return e
}

// redirectGuide is what a redirect's error tells the user to check. The
// server URL must also name the server root, the part before /api/v1.
var redirectGuide = cerrors.RedirectGuide{
	Setting: "--base-url (the Prometheus server root)",
	Steps:   []string{"prometheus-cli doctor", "prometheus-cli config get-contexts"},
}

func statusSlug(status int) string {
	if t := http.StatusText(status); t != "" {
		return strings.ToUpper(strings.ReplaceAll(t, " ", "_"))
	}
	return fmt.Sprintf("%d", status)
}

// firstLine returns the first non-empty trimmed line of s, truncated.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			if len(ln) > 200 {
				ln = ln[:200] + "…"
			}
			return ln
		}
	}
	return ""
}

// Ping verifies connectivity and credentials. It uses build info because every
// Prometheus-compatible server implements it, it is cheap, and it needs no
// query permissions.
func (c *apiClient) Ping(ctx context.Context) error {
	_, err := c.BuildInfo(ctx)
	return err
}

// BuildInfo returns the server's version and build metadata.
func (c *apiClient) BuildInfo(ctx context.Context) (*BuildInfo, error) {
	env, err := c.get(ctx, apiPath("/status/buildinfo"), nil)
	if err != nil {
		return nil, err
	}
	var info BuildInfo
	if err := decodeInto(env, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
