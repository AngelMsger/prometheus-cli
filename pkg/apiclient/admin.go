package apiclient

import (
	"context"
	"net/url"
	"strings"
	"time"

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
	sent := time.Now()
	_, err := c.post(ctx, apiPath(pathDeleteSeries), deleteSeriesForm(req))
	if ce := cerrors.AsCLIError(err); ce != nil && ce.HTTPStatus >= 300 && ce.HTTPStatus < 400 {
		// A redirect leaves the delete's outcome unknown (see httpError),
		// and no read settles it: Prometheus can keep listing a series whose
		// samples were deleted. A repeat is the user's call, so the only
		// command offered is a preview with the window fixed.
		if preview, ok := fixedWindowDeletePreview(req, sent); ok {
			ce.Hint += " Repeat it with its window fixed as in the preview below: it ends by " +
				"the first attempt, so it cannot reach samples collected since."
			ce.NextSteps = append(ce.NextSteps, preview)
		} else {
			ce.Hint += " No part of its window can be fixed before the first attempt, so no " +
				"repeat is offered."
		}
		return ce
	}
	return err
}

// fixedWindowDeletePreview is the --dry-run of req with its window fixed, for
// a repeat the user confirms. The window starts at req's start, or at the
// epoch for a delete without one, which reaches back to the oldest sample. It
// ends at req's end or at sent, the first attempt, whichever is earlier, so a
// repeat cannot reach samples collected since, whatever window req asked for.
// Both instants are RFC3339 with every fraction kept, which --from and --to
// read as written; an epoch number such as 0 would read as a zero look-back,
// that is now. ok is false when that window is empty: req starts at or after
// the first attempt, or its own end is not after its start.
func fixedWindowDeletePreview(req DeleteSeriesRequest, sent time.Time) (preview string, ok bool) {
	from := time.Unix(0, 0)
	if !req.Start.IsZero() {
		from = req.Start
	}
	to := sent
	if !req.End.IsZero() && req.End.Before(sent) {
		to = req.End
	}
	if !to.After(from) {
		return "", false
	}
	cmd := "prometheus-cli admin delete-series"
	for _, m := range req.Match {
		cmd += " --match " + cerrors.ShellArg(m)
	}
	return cmd + " --from " + previewInstant(from) + " --to " + previewInstant(to) + " --dry-run", true
}

func previewInstant(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
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
