package apiclient

import (
	"context"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// NewReadOnly wraps c so that every mutating method returns a READONLY_BLOCKED
// error before a request leaves the process. Reads (the Reader half of Client)
// pass straight through.
//
// This is how the session read-only posture (defaults.read_only /
// PROMETHEUS_CLI_READ_ONLY / --allow-writes) is enforced.
func NewReadOnly(c Client) Client {
	return &readOnlyClient{Reader: c}
}

// readOnlyClient is the read-only enforcement layer. It keeps the inner client
// only as a Reader, so reads are delegated unchanged and no write can reach it.
// Every Writer method (the TSDB admin API) is overridden below to return
// READONLY_BLOCKED; the assertion that follows stops compilation when a new
// Writer method lacks its override.
type readOnlyClient struct {
	Reader
}

var _ Client = (*readOnlyClient)(nil)

func (r *readOnlyClient) DeleteSeries(context.Context, DeleteSeriesRequest) error {
	return blocked("delete series")
}

func (r *readOnlyClient) CleanTombstones(context.Context) error {
	return blocked("clean tombstones")
}

func (r *readOnlyClient) Snapshot(context.Context, bool) (string, error) {
	return "", blocked("write a TSDB snapshot")
}

// blocked returns the family's READONLY_BLOCKED error for a refused write. op
// names the write ("delete series"). Turning read-only mode off is the user's
// decision, so the error names the user's switches without offering to change
// them. --dry-run previews still work: they never call these methods.
func blocked(op string) error {
	return cerrors.ReadOnlyBlocked(op, cerrors.ReadOnlyMode{
		Env:        "PROMETHEUS_CLI_READ_ONLY",
		ConfigFile: "~/.angelmsger/prometheus/config.yaml",
		DryRun:     true,
	})
}
