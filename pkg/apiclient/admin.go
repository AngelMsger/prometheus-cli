package apiclient

import (
	"context"
	"net/url"
	"strings"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// The three TSDB admin endpoints. They are the only mutating calls in this
// client and the only ones the read-only wrapper blocks. They exist on the
// server only when it was started with --web.enable-admin-api; a 404 on these
// paths is translated into ADMIN_API_DISABLED (see httpError).
const (
	pathDeleteSeries    = "/admin/tsdb/delete_series"
	pathCleanTombstones = "/admin/tsdb/clean_tombstones"
	pathSnapshot        = "/admin/tsdb/snapshot"
)

// deleteSeriesForm builds the request body for a delete, shared by the call and
// its --dry-run preview so a preview can never drift from what would be sent.
func deleteSeriesForm(req DeleteSeriesRequest) url.Values {
	form := url.Values{}
	addMatchers(form, req.Match)
	addWindow(form, req.Start, req.End)
	return form
}

// DeleteSeries marks the samples matching the selectors as deleted. The data
// stays on disk until a compaction or CleanTombstones removes it.
func (c *apiClient) DeleteSeries(ctx context.Context, req DeleteSeriesRequest) error {
	if len(req.Match) == 0 {
		return cerrors.New(cerrors.CategoryUsage, "NO_MATCH",
			"at least one --match selector is required to delete series").
			WithHint("Deleting without a selector would match every series. " +
				"Confirm the exact set first.").
			WithNextSteps("prometheus-cli series list --match '<selector>' --since 1h")
	}
	_, err := c.post(ctx, apiPath(pathDeleteSeries), deleteSeriesForm(req))
	return err
}

// CleanTombstones removes deleted blocks from disk immediately.
func (c *apiClient) CleanTombstones(ctx context.Context) error {
	_, err := c.post(ctx, apiPath(pathCleanTombstones), nil)
	return err
}

// Snapshot writes a TSDB snapshot and returns its directory name, relative to
// the server's data directory (snapshots/<name>).
func (c *apiClient) Snapshot(ctx context.Context, skipHead bool) (string, error) {
	env, err := c.post(ctx, apiPath(pathSnapshot), snapshotForm(skipHead))
	if err != nil {
		return "", err
	}
	var raw struct {
		Name string `json:"name"`
	}
	if err := decodeInto(env, &raw); err != nil {
		return "", err
	}
	return raw.Name, nil
}

func snapshotForm(skipHead bool) url.Values {
	if !skipHead {
		return nil
	}
	return url.Values{"skip_head": {"true"}}
}

// DeleteSeriesPlan returns the WritePlan a DeleteSeries call would execute. It
// sends nothing — it backs --dry-run as a credentials-free preview.
func DeleteSeriesPlan(baseURL string, req DeleteSeriesRequest) WritePlan {
	form := deleteSeriesForm(req)
	return WritePlan{
		DryRun: true,
		Method: "POST",
		URL:    baseURL + apiPath(pathDeleteSeries),
		Body:   form.Encode(),
		Effect: "mark every sample matching " + strings.Join(req.Match, ", ") +
			" as deleted in the selected window; the data is removed on the next compaction or clean-tombstones",
	}
}

// CleanTombstonesPlan returns the WritePlan a CleanTombstones call would execute.
func CleanTombstonesPlan(baseURL string) WritePlan {
	return WritePlan{
		DryRun: true,
		Method: "POST",
		URL:    baseURL + apiPath(pathCleanTombstones),
		Effect: "erase every already-deleted block from disk immediately; deleted samples become unrecoverable",
	}
}

// SnapshotPlan returns the WritePlan a Snapshot call would execute.
func SnapshotPlan(baseURL string, skipHead bool) WritePlan {
	plan := WritePlan{
		DryRun: true,
		Method: "POST",
		URL:    baseURL + apiPath(pathSnapshot),
		Effect: "write a TSDB snapshot under the server's data directory",
	}
	if form := snapshotForm(skipHead); form != nil {
		plan.Body = form.Encode()
		plan.Effect += " excluding the in-memory head block"
	}
	return plan
}
