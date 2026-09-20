package app

import (
	"strings"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newTargetCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Inspect scrape targets and why they are failing",
		Long: "Targets are what Prometheus scrapes. When a metric is missing the cause is\n" +
			"usually here, not in the query: a target that is down, one that was never\n" +
			"discovered, or one dropped by relabelling. Active and dropped targets come\n" +
			"back in one list with an explicit `state`, each carrying its health, last\n" +
			"scrape time (absolute and relative) and last error.",
	}
	cmd.AddCommand(newTargetListCmd(s))
	return cmd
}

func newTargetListCmd(s *appState) *cobra.Command {
	var (
		state      string
		scrapePool string
		unhealthy  bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List scrape targets, active and dropped",
		Long: "Returns the scrape targets. --state active|dropped|any narrows which the\n" +
			"server sends; --scrape-pool narrows to one pool; --unhealthy keeps only\n" +
			"the active targets Prometheus does not consider up, which is the direct\n" +
			"answer to \"what is broken right now\".\n\n" +
			"A dropped target has no post-relabelling labels, so its identity is\n" +
			"reported from `discovered_labels` — the only place the reason it was\n" +
			"dropped is visible.",
		Example: "  prometheus-cli target list --unhealthy\n" +
			"  prometheus-cli target list --state dropped\n" +
			"  prometheus-cli target list --scrape-pool node",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetState(state); err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			targets, err := client.Targets(ctx, apiclient.TargetsRequest{
				State: state, ScrapePool: scrapePool,
			})
			if err != nil {
				return err
			}
			if unhealthy {
				targets = keepUnhealthy(targets)
			}
			return s.emitList(targets, pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringVar(&state, "state", "", "which targets to return: active, dropped or any")
	f.StringVar(&scrapePool, "scrape-pool", "", "restrict to one scrape pool")
	f.BoolVar(&unhealthy, "unhealthy", false, "keep only active targets that are not up")
	enumComplete(cmd, "state", "active", "dropped", "any")
	return cmd
}

// validateTargetState rejects an unknown --state up front rather than letting
// the server answer with an empty list, which reads as "nothing is wrong".
func validateTargetState(state string) error {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "active", "dropped", "any":
		return nil
	default:
		return cerrors.Newf(cerrors.CategoryUsage, "BAD_TARGET_STATE",
			"unknown --state %q (want active, dropped or any)", state).
			WithNextSteps("prometheus-cli target list --state active")
	}
}

// keepUnhealthy filters to the active targets Prometheus does not report as up.
// Dropped targets are excluded: they are not unhealthy, they were never scraped.
func keepUnhealthy(targets []apiclient.Target) []apiclient.Target {
	out := make([]apiclient.Target, 0, len(targets))
	for _, t := range targets {
		if t.State == "active" && !strings.EqualFold(t.Health, "up") {
			out = append(out, t)
		}
	}
	return out
}
