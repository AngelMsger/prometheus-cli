package app

import (
	"strings"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newRuleCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rule",
		Short: "Inspect recording and alerting rules",
		Long: "Rules are the queries Prometheus evaluates on a schedule: recording rules\n" +
			"that precompute a series, and alerting rules that fire alerts. The CLI\n" +
			"flattens them out of their groups into one row per rule carrying `group`,\n" +
			"`file` and the group's evaluation interval, so a failing rule can be found\n" +
			"and reported without walking a nested document.",
	}
	cmd.AddCommand(newRuleListCmd(s))
	return cmd
}

func newRuleListCmd(s *appState) *cobra.Command {
	var (
		ruleType      string
		name          []string
		group         []string
		file          []string
		match         []string
		excludeAlerts bool
		failing       bool
		groupLimit    int
		cursor        string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rules, with their health and active alerts",
		Long: "Returns the configured rules. --type narrows to alerting or recording\n" +
			"rules; --name, --group and --file filter by exact identifier (discover\n" +
			"all three from an unfiltered listing); --failing keeps only the rules\n" +
			"whose last evaluation errored, which is the fastest path to a broken\n" +
			"recording rule.\n\n" +
			"Alerting rules carry their currently active alerts. On an instance with\n" +
			"many firing alerts that is the bulk of the response — pass\n" +
			"--exclude-alerts to drop it. Use --group-limit to page by rule group and\n" +
			"resume with the returned --cursor.",
		Example: "  prometheus-cli rule list --type alert\n" +
			"  prometheus-cli rule list --failing\n" +
			"  prometheus-cli rule list --group node-alerts --exclude-alerts",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateRuleType(ruleType); err != nil {
				return err
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			page, err := client.Rules(ctx, apiclient.RulesRequest{
				Type:           ruleType,
				RuleName:       name,
				RuleGroup:      group,
				File:           file,
				Match:          match,
				ExcludeAlerts:  excludeAlerts,
				GroupLimit:     groupLimit,
				GroupNextToken: cursor,
			})
			if err != nil {
				return err
			}
			rules := page.Rules
			if failing {
				rules = keepFailing(rules)
			}
			return s.emitList(rules, pageInfo{Next: page.NextToken, HasMore: page.NextToken != ""})
		},
	}
	f := cmd.Flags()
	f.StringVar(&ruleType, "type", "", "restrict to 'alert' or 'record' rules")
	f.StringArrayVar(&name, "name", nil, "restrict to rules with this exact name (repeatable)")
	f.StringArrayVar(&group, "group", nil, "restrict to this rule group (repeatable)")
	f.StringArrayVar(&file, "file", nil, "restrict to rules defined in this file (repeatable)")
	f.StringArrayVar(&match, "match", nil, "label selector applied to alerting rules' active alerts (repeatable)")
	f.BoolVar(&excludeAlerts, "exclude-alerts", false, "omit each alerting rule's active alerts")
	f.BoolVar(&failing, "failing", false, "keep only rules whose last evaluation errored")
	f.IntVar(&groupLimit, "group-limit", 0, "return at most this many rule groups per page")
	f.StringVar(&cursor, "cursor", "", "resume a --group-limit listing from the previous page's next token")
	enumComplete(cmd, "type", "alert", "record")
	return cmd
}

func validateRuleType(ruleType string) error {
	switch strings.ToLower(strings.TrimSpace(ruleType)) {
	case "", "alert", "record":
		return nil
	default:
		return cerrors.Newf(cerrors.CategoryUsage, "BAD_RULE_TYPE",
			"unknown --type %q (want alert or record)", ruleType).
			WithNextSteps("prometheus-cli rule list --type alert")
	}
}

// keepFailing filters to rules whose last evaluation errored. Prometheus keeps
// serving the rule's stale result in that state, so a silently broken recording
// rule is invisible in the data it produces.
func keepFailing(rules []apiclient.Rule) []apiclient.Rule {
	out := make([]apiclient.Rule, 0, len(rules))
	for _, r := range rules {
		if r.LastError != "" || strings.EqualFold(r.Health, "err") {
			out = append(out, r)
		}
	}
	return out
}
