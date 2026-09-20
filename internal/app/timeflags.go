package app

import (
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/timeutil"
	"github.com/spf13/cobra"
)

// timeFlags holds the shared time-window flags. They are the family's standard
// bounded-window vocabulary: --since for a look-back, --from/--to for an
// explicit range.
type timeFlags struct {
	since string
	from  string
	to    string
}

func addTimeFlags(cmd *cobra.Command, t *timeFlags) {
	f := cmd.Flags()
	f.StringVar(&t.since, "since", "", "look back this far from now, e.g. 15m, 1h, 24h, 7d")
	f.StringVar(&t.from, "from", "", "window start: RFC3339, an epoch, 2006-01-02, or now-1h")
	f.StringVar(&t.to, "to", "", "window end (default now)")
}

// given reports whether any window flag was supplied.
func (t timeFlags) given() bool {
	return timeutil.Range{Since: t.since, From: t.from, To: t.to}.Optional()
}

// resolve turns the flags into a required window.
func (t timeFlags) resolve(example string) (start, end time.Time, err error) {
	r := timeutil.Range{Since: t.since, From: t.from, To: t.to}
	start, end, rerr := r.Resolve()
	if rerr != nil {
		return time.Time{}, time.Time{}, cerrors.Wrap(rerr, cerrors.CategoryUsage, "BAD_TIME_RANGE", rerr.Error()).
			WithHint("Pass --since (e.g. 1h) or --from/--to.").
			WithNextSteps(example)
	}
	return start, end, nil
}

// resolveOptional turns the flags into a window that may be absent. The series,
// labels and metadata endpoints default to the server's full retention when no
// bound is given, which is the right behaviour for discovery — but expensive on
// a large instance, so callers pass a window when they have one.
func (t timeFlags) resolveOptional(example string) (start, end time.Time, err error) {
	if !t.given() {
		return time.Time{}, time.Time{}, nil
	}
	return t.resolve(example)
}
