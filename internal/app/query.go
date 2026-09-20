package app

import (
	"os"
	"strings"
	"time"

	"github.com/angelmsger/prometheus-cli/internal/output"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
	"github.com/spf13/cobra"
)

func newQueryCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Run PromQL queries (instant, range and exemplars)",
		Long: "Query is the heart of the CLI. `query instant` evaluates an expression at\n" +
			"a single point in time and returns one value per series; `query range`\n" +
			"evaluates it repeatedly across a window and returns the series behind a\n" +
			"graph. `query format` and `query parse` check an expression without\n" +
			"evaluating it. Results are normalized: Prometheus' positional\n" +
			"[timestamp, \"value\"] tuples come back as named fields with an RFC3339\n" +
			"instant alongside the raw epoch.\n\n" +
			"Discover what to query with `metadata list` (metric names and their help\n" +
			"text), `labels list` / `labels values` (label names and values) and\n" +
			"`series list` (the exact series a selector matches).",
	}
	cmd.AddCommand(
		newQueryInstantCmd(s),
		newQueryRangeCmd(s),
		newQueryExemplarsCmd(s),
		newQueryFormatCmd(s),
		newQueryParseCmd(s),
	)
	return cmd
}

// queryFlags holds the options shared by the instant and range queries.
type queryFlags struct {
	query        string
	queryTimeout string
	limit        int
	stats        bool
}

func addQueryFlags(cmd *cobra.Command, q *queryFlags) {
	f := cmd.Flags()
	f.StringVar(&q.query, "query", "", "PromQL expression (or @file / @- to read it from a file or stdin)")
	f.StringVar(&q.queryTimeout, "query-timeout", "",
		"server-side evaluation timeout, e.g. 10s (distinct from --timeout, which bounds the HTTP request)")
	f.IntVar(&q.limit, "limit", 0, "maximum number of series to return (0 = unlimited)")
	f.BoolVar(&q.stats, "stats", false, "include the server's query execution statistics")
	_ = cmd.MarkFlagRequired("query")
}

// resolve validates the expression and the server-side timeout.
func (q queryFlags) resolve() (promql string, timeout time.Duration, err error) {
	promql, err = resolvePromQL(q.query)
	if err != nil {
		return "", 0, err
	}
	if strings.TrimSpace(q.queryTimeout) != "" {
		timeout, err = timeutil.ParseFlexDuration(q.queryTimeout)
		if err != nil {
			return "", 0, cerrors.Wrap(err, cerrors.CategoryUsage, "BAD_QUERY_TIMEOUT",
				"invalid --query-timeout "+q.queryTimeout).
				WithHint("Use a duration such as 10s, 30s or 2m.")
		}
	}
	return promql, timeout, nil
}

func newQueryInstantCmd(s *appState) *cobra.Command {
	var (
		qf      queryFlags
		instant string
	)
	cmd := &cobra.Command{
		Use:     "instant",
		Aliases: []string{"now"},
		Short:   "Evaluate a PromQL expression at a single instant",
		Long: "Runs an instant query. The result is one value per matching series at the\n" +
			"evaluation time, which defaults to now. This is the shape to use for\n" +
			"\"what is it right now\" and for any aggregate you intend to read as a\n" +
			"single number.",
		Example: "  # current request rate per service\n" +
			"  prometheus-cli query instant --query 'sum by (service) (rate(http_requests_total[5m]))'\n\n" +
			"  # which targets are down right now\n" +
			"  prometheus-cli query instant --query 'up == 0'\n\n" +
			"  # evaluate at a past instant\n" +
			"  prometheus-cli query instant --query 'up' --time 2026-01-02T15:04:05Z",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			promql, queryTimeout, err := qf.resolve()
			if err != nil {
				return err
			}
			at, err := instantAt(instant)
			if err != nil {
				return err
			}

			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			result, err := client.Query(ctx, apiclient.InstantRequest{
				Query:   promql,
				Time:    at,
				Timeout: queryTimeout,
				Limit:   qf.limit,
				Stats:   qf.stats,
			})
			if err != nil {
				return enrichQueryError(err, promql)
			}
			return s.emitQuery(result)
		},
	}
	addQueryFlags(cmd, &qf)
	cmd.Flags().StringVar(&instant, "time", "",
		"evaluation instant: RFC3339, an epoch, 2006-01-02, or now-1h (default now)")
	return cmd
}

func newQueryRangeCmd(s *appState) *cobra.Command {
	var (
		qf   queryFlags
		tf   timeFlags
		step string
	)
	cmd := &cobra.Command{
		Use:     "range",
		Aliases: []string{"query-range"},
		Short:   "Evaluate a PromQL expression across a time window",
		Long: "Runs a range query: the expression is evaluated at every step between the\n" +
			"start and end of the window, yielding a timestamped series per result —\n" +
			"the data behind a graph. The window is required (--since, or --from/--to).\n\n" +
			"--step is optional. Prometheus rejects a range query resolving to more\n" +
			"than 11,000 points per series, so with no --step the CLI derives a round\n" +
			"resolution for the window, and an explicit --step that would overrun the\n" +
			"limit is raised rather than sent. The step actually used, its source\n" +
			"(flag / derived / clamped) and the resulting point count come back in the\n" +
			"result, so the resolution behind the numbers is never implicit.",
		Example: "  # error rate over the last hour, resolution chosen for you\n" +
			"  prometheus-cli query range --query 'sum(rate(http_requests_total{status=~\"5..\"}[5m]))' --since 1h\n\n" +
			"  # an explicit window and resolution\n" +
			"  prometheus-cli query range --query 'up' --from 2026-01-02T00:00:00Z --to 2026-01-02T06:00:00Z --step 5m\n\n" +
			"  # a long expression read from a file, streamed one series per line\n" +
			"  prometheus-cli query range --query @slo.promql --since 24h --format ndjson",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			promql, queryTimeout, err := qf.resolve()
			if err != nil {
				return err
			}
			start, end, err := tf.resolve(
				"prometheus-cli query range --query '<promql>' --since 1h")
			if err != nil {
				return err
			}
			resolved, err := timeutil.ResolveStep(start, end, step,
				constants.TargetRangePoints, constants.MaxRangePoints)
			if err != nil {
				return cerrors.Wrap(err, cerrors.CategoryUsage, "BAD_STEP", err.Error()).
					WithHint("Omit --step to let the CLI choose a resolution for the window.")
			}

			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			result, err := client.QueryRange(ctx, apiclient.RangeRequest{
				Query:   promql,
				Start:   start,
				End:     end,
				Step:    resolved.Value,
				Timeout: queryTimeout,
				Limit:   qf.limit,
				Stats:   qf.stats,
			})
			if err != nil {
				return enrichQueryError(err, promql)
			}
			result.Step = &apiclient.StepInfo{
				Value:  resolved.String(),
				Source: resolved.Source,
				Points: resolved.Points,
			}
			return s.emitQuery(result)
		},
	}
	addQueryFlags(cmd, &qf)
	addTimeFlags(cmd, &tf)
	cmd.Flags().StringVar(&step, "step", "",
		"evaluation resolution, e.g. 30s, 1m, 5m (default: derived from the window)")
	return cmd
}

func newQueryExemplarsCmd(s *appState) *cobra.Command {
	var (
		query string
		tf    timeFlags
	)
	cmd := &cobra.Command{
		Use:   "exemplars",
		Short: "List the trace exemplars attached to a selector",
		Long: "Returns the exemplars recorded alongside samples over a window — the\n" +
			"trace ids that link a metric spike to the requests that caused it. The\n" +
			"window is required, and the server must have exemplar storage enabled.",
		Example: "  prometheus-cli query exemplars --query 'http_request_duration_seconds_bucket' --since 1h",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			promql, err := resolvePromQL(query)
			if err != nil {
				return err
			}
			start, end, err := tf.resolve(
				"prometheus-cli query exemplars --query '<selector>' --since 1h")
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			sets, err := client.QueryExemplars(ctx, apiclient.ExemplarRequest{
				Query: promql, Start: start, End: end,
			})
			if err != nil {
				return enrichQueryError(err, promql)
			}
			return s.emitList(sets, pageInfo{})
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "series selector (or @file / @- to read it from a file or stdin)")
	_ = cmd.MarkFlagRequired("query")
	addTimeFlags(cmd, &tf)
	return cmd
}

func newQueryFormatCmd(s *appState) *cobra.Command {
	var query string
	cmd := &cobra.Command{
		Use:   "format",
		Short: "Pretty-print a PromQL expression (and confirm it parses)",
		Long: "Asks the server to re-render an expression in its canonical form. It is\n" +
			"the cheapest way to confirm an expression is syntactically valid before\n" +
			"running it against a large window.",
		Example: "  prometheus-cli query format --query 'sum  by(job)(rate(  up[5m] ))'",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			promql, err := resolvePromQL(query)
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			formatted, err := client.FormatQuery(ctx, promql)
			if err != nil {
				return enrichQueryError(err, promql)
			}
			return s.emit(map[string]any{"query": promql, "formatted": formatted})
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "PromQL expression (or @file / @- to read it from a file or stdin)")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}

func newQueryParseCmd(s *appState) *cobra.Command {
	var query string
	cmd := &cobra.Command{
		Use:   "parse",
		Short: "Show the server's parse tree for a PromQL expression",
		Long: "Returns the abstract syntax tree Prometheus builds for an expression.\n" +
			"Use it to see exactly how an expression was interpreted — which operator\n" +
			"binds where, what a selector's matchers resolved to — when a query\n" +
			"returns something other than what was intended.",
		Example: "  prometheus-cli query parse --query 'rate(http_requests_total[5m]) > 0.5'",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			promql, err := resolvePromQL(query)
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			tree, err := client.ParseQuery(ctx, promql)
			if err != nil {
				return enrichQueryError(err, promql)
			}
			return s.emit(map[string]any{"query": promql, "ast": tree})
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "PromQL expression (or @file / @- to read it from a file or stdin)")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}

// emitQuery renders a query result. JSON returns the whole normalized document;
// ndjson streams one series per line, for a result too large to hold in one
// blob. The server's own warnings are re-emitted on stderr in ndjson mode so
// they are not lost when only the series are piped onward.
func (s *appState) emitQuery(result *apiclient.QueryResult) error {
	if s.ndjson() {
		emitQueryAdvisories(result)
		return s.emitList(result.Series, pageInfo{})
	}
	return s.emit(result)
}

// resolvePromQL resolves the --query flag (supporting @file / @-) and rejects an
// empty expression with discovery guidance.
func resolvePromQL(query string) (string, error) {
	promql, err := readInlineOrFile(query)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(promql) == "" {
		return "", cerrors.New(cerrors.CategoryUsage, "NO_QUERY", "--query is empty").
			WithHint("Pass a PromQL expression, or @<path> to read one from a file.").
			WithNextSteps(
				"prometheus-cli metadata list",
				"prometheus-cli labels values __name__ --since 1h")
	}
	return promql, nil
}

// instantAt resolves the --time flag. The zero value means "now", which the
// server applies when the parameter is absent.
func instantAt(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	t, err := timeutil.ParseInstant(value, time.Now())
	if err != nil {
		return time.Time{}, cerrors.Wrap(err, cerrors.CategoryUsage, "BAD_TIME",
			"invalid --time "+value+": "+err.Error()).
			WithHint("Use RFC3339, a unix epoch, 2006-01-02, or now-1h.")
	}
	return t, nil
}

// enrichQueryError points a rejected query at metric and label discovery. The
// overwhelmingly common cause is a metric name or label that does not exist on
// this server, which no amount of retrying fixes — so the recovery path is a
// discovery command, not a retry.
func enrichQueryError(err error, promql string) error {
	ce := cerrors.AsCLIError(err)
	if ce.Category != cerrors.CategoryUsage && ce.Category != cerrors.CategoryParse {
		return err
	}
	if ce.Hint == "" {
		ce.Hint = "Check the metric name and label matchers — PromQL is not SQL."
	}
	ce.NextSteps = []string{
		"prometheus-cli query parse --query '" + shellQuote(promql) + "'",
		"prometheus-cli metadata list --metric <name>",
		"prometheus-cli labels values __name__ --since 1h",
		"prometheus-cli series list --match '<selector>' --since 1h",
	}
	return ce
}

// shellQuote makes an expression safe to paste back inside the single quotes of
// a suggested next step.
func shellQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'"'"'`)
}

// emitQueryAdvisories re-emits a result's server warnings and infos on stderr.
// In ndjson mode only the series reach stdout, and a warning such as "results
// are truncated" changes how the numbers should be read — losing it silently
// would be a wrong answer, so it is surfaced as a structured notice.
func emitQueryAdvisories(result *apiclient.QueryResult) {
	if len(result.Warnings) == 0 && len(result.Infos) == 0 {
		return
	}
	notice := map[string]any{"query": result.Query}
	if len(result.Warnings) > 0 {
		notice["warnings"] = result.Warnings
	}
	if len(result.Infos) > 0 {
		notice["infos"] = result.Infos
	}
	output.EmitNotice(os.Stderr, map[string]any{"_notice": map[string]any{"query_advisories": notice}})
}
