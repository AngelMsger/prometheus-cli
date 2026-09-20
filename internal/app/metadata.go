package app

import (
	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	"github.com/spf13/cobra"
)

func newMetadataCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metadata",
		Short: "Look up what a metric is: its type, help text and unit",
		Long: "Metric metadata is the difference between guessing at a metric and knowing\n" +
			"it. The type decides how it must be queried — a counter needs rate(), a\n" +
			"gauge does not — and the help text says what it actually measures.\n" +
			"`metadata list` reads the server's aggregated view; `metadata targets`\n" +
			"reads it per scrape target, which is how you find a disagreement between\n" +
			"two exporters of the same metric name.",
	}
	cmd.AddCommand(newMetadataListCmd(s), newMetadataTargetsCmd(s))
	return cmd
}

func newMetadataListCmd(s *appState) *cobra.Command {
	var (
		metric         string
		limit          int
		limitPerMetric int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List metric metadata (type, help, unit)",
		Long: "Returns one row per metric name with its type, help text and unit, sorted\n" +
			"by name. Narrow to a single metric with --metric before writing a query\n" +
			"against it.\n\n" +
			"--limit defaults to a bounded page; pass --limit 0 to lift it.",
		Example: "  prometheus-cli metadata list --metric http_requests_total\n" +
			"  prometheus-cli metadata list --limit 100",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			items, err := client.Metadata(ctx, apiclient.MetadataRequest{
				Metric: metric, Limit: limit, LimitPerMetric: limitPerMetric,
			})
			if err != nil {
				return err
			}
			return s.emitList(items, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringVar(&metric, "metric", "", "restrict to one metric name")
	f.IntVar(&limit, "limit", constants.DefaultMetadataLimit, "maximum number of metric names to return (0 = unlimited)")
	f.IntVar(&limitPerMetric, "limit-per-metric", 0, "maximum entries per metric name (0 = unlimited)")
	return cmd
}

func newMetadataTargetsCmd(s *appState) *cobra.Command {
	var (
		matchTarget string
		metric      string
		limit       int
	)
	cmd := &cobra.Command{
		Use:   "targets",
		Short: "Show metric metadata as individual scrape targets report it",
		Long: "Returns metadata per target rather than aggregated, each row carrying the\n" +
			"target's labels. Use it when the same metric name is exported by more than\n" +
			"one job and you need to know which definition a series came from.",
		Example: "  prometheus-cli metadata targets --metric http_requests_total\n" +
			"  prometheus-cli metadata targets --match-target '{job=\"node\"}'",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			items, err := client.TargetMetadata(ctx, apiclient.TargetMetadataRequest{
				MatchTarget: matchTarget, Metric: metric, Limit: limit,
			})
			if err != nil {
				return err
			}
			return s.emitList(items, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringVar(&matchTarget, "match-target", "", "target label selector, e.g. '{job=\"node\"}' (list targets with `target list`)")
	f.StringVar(&metric, "metric", "", "restrict to one metric name")
	f.IntVar(&limit, "limit", constants.DefaultMetadataLimit, "maximum number of entries to return (0 = unlimited)")
	return cmd
}
