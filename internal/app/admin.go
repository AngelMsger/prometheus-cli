package app

import (
	"strings"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newAdminCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "TSDB administration: delete series, clean tombstones, snapshot (writes)",
		Long: "The TSDB admin endpoints are the only commands in this CLI that change\n" +
			"anything. They exist on the server only when it was started with\n" +
			"--web.enable-admin-api; without it every one of them returns\n" +
			"ADMIN_API_DISABLED (confirm with `status flags`).\n\n" +
			"All three are writes: preview with --dry-run, and they are blocked in\n" +
			"read-only mode unless --allow-writes is given. The two that destroy data\n" +
			"(delete-series, clean-tombstones) additionally require --yes, and never\n" +
			"prompt — so a non-interactive caller fails with a structured error\n" +
			"instead of hanging.",
	}
	cmd.AddCommand(
		newAdminDeleteSeriesCmd(s),
		newAdminCleanTombstonesCmd(s),
		newAdminSnapshotCmd(s),
	)
	return cmd
}

func newAdminDeleteSeriesCmd(s *appState) *cobra.Command {
	var (
		match  []string
		tf     timeFlags
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "delete-series",
		Short: "Mark the samples matching a selector as deleted (destructive write)",
		Long: "Marks every sample matching the selectors as deleted, optionally within a\n" +
			"window. The data stays on disk until the next compaction or a\n" +
			"`clean-tombstones`, but it stops being queryable immediately.\n\n" +
			"A selector is required: deleting without one would match everything.\n" +
			"Confirm the exact set with `series list --match ...` first — the preview\n" +
			"that --dry-run prints is the request, not the affected series.",
		Example: "  # confirm what the selector matches, preview, then execute\n" +
			"  prometheus-cli series list --match 'up{job=\"retired\"}' --since 30d\n" +
			"  prometheus-cli admin delete-series --match 'up{job=\"retired\"}' --dry-run\n" +
			"  prometheus-cli admin delete-series --match 'up{job=\"retired\"}' --yes",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			start, end, err := tf.resolveOptional(
				"prometheus-cli admin delete-series --match '<selector>' --since 24h --dry-run")
			if err != nil {
				return err
			}
			req := apiclient.DeleteSeriesRequest{Match: match, Start: start, End: end}

			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			if dryRun {
				return s.emit(apiclient.DeleteSeriesPlan(client.BaseURL(), req))
			}
			if err := requireConfirmation(yes, "delete series",
				"deleting samples matching "+strings.Join(match, ", ")); err != nil {
				return err
			}
			if err := client.DeleteSeries(ctx, req); err != nil {
				return err
			}
			return s.emit(map[string]any{"deleted": true, "match": match})
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&match, "match", nil, "series selector identifying what to delete (repeatable, required)")
	f.BoolVar(&dryRun, "dry-run", false, "print the request without sending it")
	f.BoolVar(&yes, "yes", false, "confirm this destructive write")
	addTimeFlags(cmd, &tf)
	_ = cmd.MarkFlagRequired("match")
	return cmd
}

func newAdminCleanTombstonesCmd(s *appState) *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "clean-tombstones",
		Short: "Erase already-deleted blocks from disk (destructive write)",
		Long: "Removes the blocks that `delete-series` marked, immediately and\n" +
			"irreversibly, instead of waiting for the next compaction. It takes no\n" +
			"selector: it applies to everything currently marked deleted on this\n" +
			"server.",
		Example: "  prometheus-cli admin clean-tombstones --dry-run\n" +
			"  prometheus-cli admin clean-tombstones --yes",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			if dryRun {
				return s.emit(apiclient.CleanTombstonesPlan(client.BaseURL()))
			}
			if err := requireConfirmation(yes, "clean tombstones",
				"permanently erasing every block already marked deleted on this server"); err != nil {
				return err
			}
			if err := client.CleanTombstones(ctx); err != nil {
				return err
			}
			return s.emit(map[string]any{"cleaned": true})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&dryRun, "dry-run", false, "print the request without sending it")
	f.BoolVar(&yes, "yes", false, "confirm this destructive write")
	return cmd
}

func newAdminSnapshotCmd(s *appState) *cobra.Command {
	var (
		skipHead bool
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Write a TSDB snapshot (write)",
		Long: "Writes a snapshot of the current data under the server's data directory\n" +
			"and returns its name. It adds data rather than destroying it, so unlike\n" +
			"the other admin commands it needs no --yes — but it does consume disk on\n" +
			"the server, and it is still blocked in read-only mode.",
		Example: "  prometheus-cli admin snapshot --dry-run\n" +
			"  prometheus-cli admin snapshot --skip-head",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				return err
			}
			if dryRun {
				return s.emit(apiclient.SnapshotPlan(client.BaseURL(), skipHead))
			}
			name, err := client.Snapshot(ctx, skipHead)
			if err != nil {
				return err
			}
			return s.emit(map[string]any{"snapshot": true, "name": name, "skip_head": skipHead})
		},
	}
	f := cmd.Flags()
	f.BoolVar(&skipHead, "skip-head", false, "exclude the in-memory head block from the snapshot")
	f.BoolVar(&dryRun, "dry-run", false, "print the request without sending it")
	return cmd
}

// requireConfirmation gates a destructive write on an explicit --yes. It never
// prompts: an agent or CI run has no terminal to answer on, and a command that
// blocks forever is worse than one that fails with instructions.
func requireConfirmation(confirmed bool, op, effect string) error {
	if confirmed {
		return nil
	}
	return cerrors.Newf(cerrors.CategoryUsage, "CONFIRMATION_REQUIRED",
		"refusing to %s without --yes", op).
		WithHint("This is irreversible: "+effect+".").
		WithNextSteps(
			"Re-run with --dry-run to see the exact request",
			"Re-run with --yes once the effect is confirmed")
}
