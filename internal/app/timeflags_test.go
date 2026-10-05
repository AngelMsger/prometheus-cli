package app

import (
	"strings"
	"testing"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// An ambiguous window is one structured usage error whether the command
// requires a window (query range) or merely accepts one (discovery, delete).
func TestTimeFlagsRejectAmbiguousWindows(t *testing.T) {
	t.Parallel()
	const example = "prometheus-cli query range --query '<promql>' --since 1h"
	for _, tc := range []struct {
		name string
		tf   timeFlags
		want string
	}{
		{"since with from", timeFlags{since: "1h", from: "2026-09-01"}, "--since cannot be combined with --from or --to"},
		{"since with to", timeFlags{since: "1h", to: "2026-09-02"}, "--since cannot be combined with --from or --to"},
		{"to without from", timeFlags{to: "2026-09-02"}, "--to requires --from"},
	} {
		for mode, resolve := range map[string]func(string) error{
			"required": func(e string) error { _, _, err := tc.tf.resolve(e); return err },
			"optional": func(e string) error { _, _, err := tc.tf.resolveOptional(e); return err },
		} {
			err := resolve(example)
			if err == nil {
				t.Fatalf("%s (%s): accepted an ambiguous window", tc.name, mode)
			}
			ce := cerrors.AsCLIError(err)
			if ce.Category != cerrors.CategoryUsage || ce.Code != "BAD_TIME_RANGE" || cerrors.ExitCode(ce) != cerrors.ExitUsage {
				t.Fatalf("%s (%s): got %s/%s exit %d", tc.name, mode, ce.Category, ce.Code, cerrors.ExitCode(ce))
			}
			if !strings.Contains(ce.Message, tc.want) || !strings.Contains(ce.Hint, "--to requires --from") ||
				len(ce.NextSteps) != 1 || ce.NextSteps[0] != example {
				t.Fatalf("%s (%s): unhelpful error: %+v", tc.name, mode, ce)
			}
		}
	}
}

// A window that is absent stays absent on the commands where it is optional,
// and a valid one still resolves: the rules reject combinations, not flags.
func TestTimeFlagsKeepValidAndAbsentWindows(t *testing.T) {
	t.Parallel()
	start, end, err := timeFlags{}.resolveOptional("example")
	if err != nil || !start.IsZero() || !end.IsZero() {
		t.Fatalf("an absent optional window resolved to %s..%s (%v)", start, end, err)
	}
	if _, _, err := (timeFlags{}).resolve("example"); err == nil {
		t.Fatal("a required window accepted no bound")
	}
	for _, tf := range []timeFlags{
		{since: "1h"},
		{from: "now-2h"},
		{from: "2026-09-01T00:00:00Z", to: "2026-09-02T00:00:00Z"},
	} {
		start, end, err := tf.resolveOptional("example")
		if err != nil || !end.After(start) {
			t.Fatalf("%+v: %s..%s (%v)", tf, start, end, err)
		}
	}
}
