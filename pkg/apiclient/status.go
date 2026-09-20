package apiclient

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// statusTopics maps a `status <topic>` subcommand to its API path segment.
// The CLI exposes the topics rather than a free-form path so every accepted
// value is discoverable and a typo fails with the list instead of a 404.
var statusTopics = map[string]string{
	"config":        "config",
	"flags":         "flags",
	"runtimeinfo":   "runtimeinfo",
	"buildinfo":     "buildinfo",
	"tsdb":          "tsdb",
	"wal-replay":    "walreplay",
	"notifications": "notifications",
}

// StatusTopics lists the supported status topics in a stable order.
func StatusTopics() []string {
	out := make([]string, 0, len(statusTopics))
	for name := range statusTopics {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Status returns one server status document as decoded JSON. Each topic has a
// different shape (config is a YAML blob, flags a string map, tsdb a set of
// cardinality tables), so the payload is passed through verbatim.
func (c *apiClient) Status(ctx context.Context, topic string, query url.Values) (json.RawMessage, error) {
	segment, ok := statusTopics[topic]
	if !ok {
		return nil, cerrors.Newf(cerrors.CategoryUsage, "UNKNOWN_STATUS_TOPIC",
			"unknown status topic %q", topic).
			WithHint("Supported topics: " + strings.Join(StatusTopics(), ", ")).
			WithNextSteps("prometheus-cli status --help")
	}
	env, err := c.get(ctx, apiPath("/status/"+segment), query)
	if err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return json.RawMessage("{}"), nil
	}
	return env.Data, nil
}
