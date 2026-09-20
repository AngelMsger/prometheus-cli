package app

import (
	"strings"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	"github.com/spf13/cobra"
)

func newAlertCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Inspect active alerts and the Alertmanagers they go to",
		Long: "`alert list` is what is firing (or pending) right now, straight from\n" +
			"Prometheus rather than from Alertmanager — so it shows alerts before any\n" +
			"grouping, inhibition or silencing is applied. `alert managers` shows the\n" +
			"Alertmanager endpoints notifications are being delivered to, which is\n" +
			"where a firing alert that never arrived is usually explained.",
	}
	cmd.AddCommand(newAlertListCmd(s), newAlertManagersCmd(s))
	return cmd
}

func newAlertListCmd(s *appState) *cobra.Command {
	var (
		state string
		name  string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the currently active alerts",
		Long: "Returns each active alert with its state (pending or firing), its labels\n" +
			"and annotations, when it became active and how long ago that was.\n" +
			"--state and --name filter the result client-side; the endpoint itself\n" +
			"takes no parameters.",
		Example: "  prometheus-cli alert list\n" +
			"  prometheus-cli alert list --state firing\n" +
			"  prometheus-cli alert list --name HighRequestLatency",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			alerts, err := client.Alerts(ctx)
			if err != nil {
				return err
			}
			return s.emitList(filterAlerts(alerts, state, name), pageInfo{})
		},
	}
	f := cmd.Flags()
	f.StringVar(&state, "state", "", "keep only alerts in this state: pending or firing")
	f.StringVar(&name, "name", "", "keep only alerts with this alertname (list them with `rule list --type alert`)")
	enumComplete(cmd, "state", "pending", "firing")
	return cmd
}

func newAlertManagersCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "managers",
		Aliases: []string{"alertmanagers"},
		Short:   "List the Alertmanager endpoints Prometheus is sending to",
		Long: "Returns the discovered Alertmanagers, active and dropped, in one list with\n" +
			"an explicit `state`. An empty active list means notifications are going\n" +
			"nowhere, however loudly the rules are firing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			items, err := client.Alertmanagers(ctx)
			if err != nil {
				return err
			}
			return s.emitList(items, pageInfo{})
		},
	}
	return cmd
}

// filterAlerts applies the client-side state and name filters.
func filterAlerts(alerts []apiclient.Alert, state, name string) []apiclient.Alert {
	if state == "" && name == "" {
		return alerts
	}
	out := make([]apiclient.Alert, 0, len(alerts))
	for _, a := range alerts {
		if state != "" && !strings.EqualFold(a.State, state) {
			continue
		}
		if name != "" && !strings.EqualFold(a.Name, name) {
			continue
		}
		out = append(out, a)
	}
	return out
}
