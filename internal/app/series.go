package app

import (
	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	"github.com/spf13/cobra"
)

func newSeriesCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "series",
		Short: "Discover the series a selector matches",
		Long: "A series is one unique label set. `series list` answers \"what exactly\n" +
			"does this selector match\" without evaluating any samples — the step to\n" +
			"take before a query that returns nothing, or one that would return far\n" +
			"more series than expected.",
	}
	cmd.AddCommand(newSeriesListCmd(s))
	return cmd
}

func newSeriesListCmd(s *appState) *cobra.Command {
	var (
		match []string
		tf    timeFlags
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the label sets matching one or more selectors",
		Long: "Returns one entry per matching series: its __name__ lifted out as `name`,\n" +
			"and the full label set as `metric`. Pass --match more than once to union\n" +
			"several selectors. The window is optional but recommended: without it the\n" +
			"server scans its whole retention.\n\n" +
			"--limit defaults to a bounded page so a stray call on a large instance\n" +
			"cannot flood an agent's context; pass --limit 0 to lift it.",
		Example: "  prometheus-cli series list --match 'up{job=\"node\"}' --since 1h\n" +
			"  prometheus-cli series list --match 'http_requests_total' --match 'up' --since 6h --limit 50",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			start, end, err := tf.resolveOptional(
				"prometheus-cli series list --match '<selector>' --since 1h")
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			items, err := client.Series(ctx, apiclient.SeriesRequest{
				Match: match, Start: start, End: end, Limit: limit,
			})
			if err != nil {
				return err
			}
			return s.emitList(items, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&match, "match", nil, "series selector, e.g. 'up{job=\"node\"}' (repeatable)")
	f.IntVar(&limit, "limit", constants.DefaultSeriesLimit, "maximum number of series to return (0 = unlimited)")
	addTimeFlags(cmd, &tf)
	_ = cmd.MarkFlagRequired("match")
	return cmd
}

func newLabelsCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "labels",
		Short: "Discover label names and their values",
		Long: "Labels are how every series is addressed. `labels list` returns the label\n" +
			"names present on a server (or within a selector); `labels values <name>`\n" +
			"returns the values one label takes. Metric names live in the reserved\n" +
			"`__name__` label, so `labels values __name__` is the full metric list.",
	}
	cmd.AddCommand(newLabelsListCmd(s), newLabelValuesCmd(s))
	return cmd
}

func newLabelsListCmd(s *appState) *cobra.Command {
	var (
		match []string
		tf    timeFlags
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List label names",
		Long: "Returns every label name the server knows, or — with --match — only the\n" +
			"names present on the series a selector matches, which is the useful form\n" +
			"when composing a `by (...)` or `without (...)` clause.",
		Example: "  prometheus-cli labels list --since 1h\n" +
			"  prometheus-cli labels list --match 'http_requests_total' --since 1h",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			start, end, err := tf.resolveOptional("prometheus-cli labels list --since 1h")
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			names, err := client.LabelNames(ctx, apiclient.LabelsRequest{
				Match: match, Start: start, End: end, Limit: limit,
			})
			if err != nil {
				return err
			}
			return s.emitList(names, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&match, "match", nil, "restrict to the series this selector matches (repeatable)")
	f.IntVar(&limit, "limit", constants.DefaultSeriesLimit, "maximum number of names to return (0 = unlimited)")
	addTimeFlags(cmd, &tf)
	return cmd
}

func newLabelValuesCmd(s *appState) *cobra.Command {
	var (
		match []string
		tf    timeFlags
		limit int
	)
	cmd := &cobra.Command{
		Use:   "values <label>",
		Short: "List the values a label takes",
		Long: "Returns the distinct values of one label. `labels values __name__` is the\n" +
			"list of metric names on the server — the starting point when you do not\n" +
			"yet know what is being recorded. Narrow with --match to see only the\n" +
			"values present on a particular set of series.",
		Example: "  # every metric name on the server\n" +
			"  prometheus-cli labels values __name__ --since 1h\n\n" +
			"  # the jobs that report a particular metric\n" +
			"  prometheus-cli labels values job --match 'http_requests_total' --since 1h",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start, end, err := tf.resolveOptional("prometheus-cli labels values __name__ --since 1h")
			if err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			values, err := client.LabelValues(ctx, args[0], apiclient.LabelsRequest{
				Match: match, Start: start, End: end, Limit: limit,
			})
			if err != nil {
				return err
			}
			return s.emitList(values, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&match, "match", nil, "restrict to the series this selector matches (repeatable)")
	f.IntVar(&limit, "limit", constants.DefaultSeriesLimit, "maximum number of values to return (0 = unlimited)")
	addTimeFlags(cmd, &tf)
	return cmd
}
