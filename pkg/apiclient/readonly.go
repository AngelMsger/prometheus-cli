package apiclient

import (
	"context"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// readOnlyClient wraps a Client and blocks every mutating method before a
// request leaves the process. Reads pass straight through; the three TSDB admin
// methods (DeleteSeries, CleanTombstones, Snapshot) return a structured
// READONLY_BLOCKED error.
//
// This is how the session read-only posture (defaults.read_only /
// PROMETHEUS_CLI_READ_ONLY / --allow-writes) is enforced. As the write surface
// grows, each new method gets one override here and nothing else changes.
type readOnlyClient struct {
	Client
}

// NewReadOnly returns a read-only view of c.
func NewReadOnly(c Client) Client {
	return &readOnlyClient{Client: c}
}

func (r *readOnlyClient) DeleteSeries(context.Context, DeleteSeriesRequest) error {
	return blocked("delete series")
}

func (r *readOnlyClient) CleanTombstones(context.Context) error {
	return blocked("clean tombstones")
}

func (r *readOnlyClient) Snapshot(context.Context, bool) (string, error) {
	return "", blocked("write a TSDB snapshot")
}

// blocked builds the structured error returned for a write attempt in read-only
// mode. Note --dry-run previews still work: they never call these methods.
func blocked(op string) error {
	return cerrors.Newf(cerrors.CategoryPermission, "READONLY_BLOCKED",
		"refusing to %s: read-only mode is enabled", op).
		WithHint("This session is read-only (defaults.read_only / PROMETHEUS_CLI_READ_ONLY). "+
			"Preview the request with --dry-run, or re-run with --allow-writes to override.").
		WithNextSteps(
			"Add --dry-run to preview the request without sending it",
			"Add --allow-writes to override read-only mode for this command",
			"unset PROMETHEUS_CLI_READ_ONLY / set defaults.read_only=false to disable it")
}
