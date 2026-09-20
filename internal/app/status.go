package app

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	"github.com/spf13/cobra"
)

func newStatusCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Read the server's configuration, flags and storage stats",
		Long: "Status answers questions about the server rather than the data: what it\n" +
			"was configured with, which flags it was started with (including whether\n" +
			"the admin API is enabled), what its storage looks like, and whether it is\n" +
			"still replaying its write-ahead log. Each topic is a separate subcommand\n" +
			"so every accepted value is discoverable from --help.",
	}
	for _, topic := range apiclient.StatusTopics() {
		cmd.AddCommand(newStatusTopicCmd(s, topic))
	}
	return cmd
}

// statusTopicDoc gives each topic its own short/long help. Sharing one generic
// description would make the subcommands indistinguishable in the reference
// docs, which is where an agent picks between them.
var statusTopicDoc = map[string][2]string{
	"config": {
		"Show the server's running configuration (YAML)",
		"Returns the configuration Prometheus is currently running, as a YAML\n" +
			"document under `yaml`. This is the reloaded state, not what is on disk —\n" +
			"the two differ whenever a change has not been reloaded.",
	},
	"flags": {
		"Show the command-line flags the server was started with",
		"Returns the server's flag values. This is where to confirm the retention\n" +
			"period, the storage path, and whether --web.enable-admin-api is on (the\n" +
			"admin commands need it).",
	},
	"runtimeinfo": {
		"Show runtime state: uptime, reload time, goroutines, storage retention",
		"Returns live runtime state including start time, last successful\n" +
			"configuration reload, and current time-series counts. A last-reload time\n" +
			"older than a config change means the change has not taken effect.",
	},
	"buildinfo": {
		"Show the server's version and build metadata",
		"Returns the version, revision and Go build details. The version decides\n" +
			"which API endpoints and PromQL features exist, so it is worth reading\n" +
			"before concluding that a missing endpoint is a configuration problem.",
	},
	"tsdb": {
		"Show storage cardinality: the biggest metrics, labels and label values",
		"Returns the head block's cardinality tables — the series count by metric\n" +
			"name, by label pair, and the labels consuming the most memory. This is the\n" +
			"direct answer to \"what is making this server slow or large\".",
	},
	"wal-replay": {
		"Show write-ahead-log replay progress",
		"Returns WAL replay progress. A server still replaying answers queries with\n" +
			"incomplete data, so a surprising empty result right after a restart is\n" +
			"explained here rather than in the query.",
	},
	"notifications": {
		"Show the server's active notifications",
		"Returns the notifications Prometheus is surfacing about itself — the same\n" +
			"banners its web UI shows. Available on newer servers only.",
	},
}

func newStatusTopicCmd(s *appState, topic string) *cobra.Command {
	var limit int
	doc := statusTopicDoc[topic]
	cmd := &cobra.Command{
		Use:   topic,
		Short: doc[0],
		Long:  doc[1],
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			query := url.Values{}
			if topic == "tsdb" && limit > 0 {
				query.Set("limit", fmt.Sprint(limit))
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			data, err := client.Status(ctx, topic, query)
			if err != nil {
				return err
			}
			return s.emit(json.RawMessage(data))
		},
	}
	if topic == "tsdb" {
		cmd.Flags().IntVar(&limit, "limit", 0,
			"number of entries per cardinality table (0 = the server's default)")
	}
	return cmd
}
