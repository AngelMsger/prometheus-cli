package timeutil

import (
	"strings"
	"testing"
	"time"
)

var reference = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func TestParseInstantAcceptsTheFormsAnAgentWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"now", reference},
		{"now-1h", reference.Add(-time.Hour)},
		{"now + 30m", reference.Add(30 * time.Minute)},
		{"2h", reference.Add(-2 * time.Hour)},
		{"7d", reference.Add(-7 * 24 * time.Hour)},
		{"2026-09-20T06:00:00Z", time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)},
		{"2026-09-20", time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)},
		{"2026-09-20 06:30:00", time.Date(2026, 9, 20, 6, 30, 0, 0, time.UTC)},
		{"1758326400", time.Unix(1758326400, 0).UTC()},
		{"1758326400000", time.UnixMilli(1758326400000).UTC()},
	} {
		got, err := ParseInstant(tc.in, reference)
		if err != nil {
			t.Fatalf("ParseInstant(%q) error: %v", tc.in, err)
		}
		if !got.Equal(tc.want) {
			t.Errorf("ParseInstant(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// Prometheus itself writes fractional Unix seconds, so a timestamp copied out
// of a previous result must parse back in.
func TestParseInstantAcceptsPrometheusFractionalSeconds(t *testing.T) {
	t.Parallel()
	got, err := ParseInstant("1758326400.5", reference)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.UnixMilli(1758326400500).UTC(); !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestParseInstantRejectsNonsense(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "yesterday", "5 fortnights", "2026-13-40"} {
		if _, err := ParseInstant(in, reference); err == nil {
			t.Errorf("ParseInstant(%q) accepted an invalid instant", in)
		}
	}
}

func TestParseFlexDurationUnderstandsPrometheusUnits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"2h", 2 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1y", 365 * 24 * time.Hour},
	} {
		got, err := ParseFlexDuration(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseFlexDuration(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
}

func TestRangeResolve(t *testing.T) {
	t.Parallel()
	t.Run("since", func(t *testing.T) {
		start, end, err := Range{Since: "1h", Now: reference}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if !end.Equal(reference) || !start.Equal(reference.Add(-time.Hour)) {
			t.Fatalf("got %s..%s", start, end)
		}
	})
	t.Run("from defaults the end to now", func(t *testing.T) {
		start, end, err := Range{From: "now-2h", Now: reference}.Resolve()
		if err != nil {
			t.Fatal(err)
		}
		if !start.Equal(reference.Add(-2*time.Hour)) || !end.Equal(reference) {
			t.Fatalf("got %s..%s", start, end)
		}
	})
	t.Run("rejects an empty or reversed window", func(t *testing.T) {
		for _, r := range []Range{
			{Now: reference},
			{From: "now", To: "now-1h", Now: reference},
			{Since: "0s", Now: reference},
		} {
			if _, _, err := r.Resolve(); err == nil {
				t.Errorf("Resolve(%+v) accepted an unusable window", r)
			}
		}
	})
}

// The family time-window contract: --since is a look-back that excludes
// --from/--to, and --to needs a --from to bound. A window that breaks either
// rule must fail instead of resolving to something the caller did not ask for
// — --since silently winning over an explicit range is a different window, and
// on `admin delete-series` a different set of deleted samples.
func TestRangeResolveRejectsAmbiguousOrUnusableWindows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		r    Range
		want string
	}{
		{"since with from", Range{Since: "24h", From: "2026-09-03"}, "--since cannot be combined"},
		{"since with to", Range{Since: "24h", To: "2026-09-03"}, "--since cannot be combined"},
		{"since with from and to", Range{Since: "24h", From: "2026-09-03", To: "2026-09-04"}, "--since cannot be combined"},
		{"to without from", Range{To: "2026-09-04"}, "--to requires --from"},
		{"no bound", Range{}, "no time range given"},
		{"reversed", Range{From: "now", To: "now-1h"}, "empty time range"},
		{"empty", Range{From: "2026-09-03", To: "2026-09-03"}, "empty time range"},
		{"zero since", Range{Since: "0s"}, "invalid --since"},
		{"negative since", Range{Since: "-1h"}, "invalid --since"},
		{"date as since", Range{Since: "2026-09-03"}, "invalid --since"},
		{"bad from", Range{From: "soon"}, "invalid --from"},
		{"bad to", Range{From: "2026-09-03", To: "later"}, "invalid --to"},
		{"from past the default end", Range{From: "now+1h"}, "empty time range"},
	} {
		tc.r.Now = reference
		_, _, err := tc.r.Resolve()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Resolve error = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

// The exclusivity rules reject combinations, not the flags themselves: each
// valid spelling of a window still resolves to the bounds it names.
func TestRangeResolveKeepsEveryValidWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		r          Range
		start, end time.Time
	}{
		{"since", Range{Since: "7d"}, reference.Add(-7 * 24 * time.Hour), reference},
		{"from alone ends now", Range{From: "2026-09-19"}, time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC), reference},
		{"from and to", Range{From: "2026-09-18T00:00:00+08:00", To: "2026-09-19"},
			time.Date(2026, 9, 17, 16, 0, 0, 0, time.UTC), time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)},
	} {
		tc.r.Now = reference
		start, end, err := tc.r.Resolve()
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !start.Equal(tc.start) || !end.Equal(tc.end) {
			t.Errorf("%s: got %s..%s, want %s..%s", tc.name, start, end, tc.start, tc.end)
		}
	}
}

// A sample timestamp must survive the round trip the CLI performs on every
// value it renders: fractional seconds in, RFC3339 out, same instant.
func TestSecondsRoundTrip(t *testing.T) {
	t.Parallel()
	const secs = 1758326400.5
	if got := FromSeconds(secs); !got.Equal(time.UnixMilli(1758326400500).UTC()) {
		t.Fatalf("FromSeconds = %s", got)
	}
	if got := RenderInstant(secs); got != "2025-09-20T00:00:00.5Z" {
		t.Fatalf("RenderInstant = %q", got)
	}
	if got := FormatInstant(time.Unix(1758326400, 0)); got != "1758326400" {
		t.Fatalf("FormatInstant = %q", got)
	}
}

func TestRenderInstantIgnoresNonInstants(t *testing.T) {
	t.Parallel()
	for _, v := range []float64{nan(), inf()} {
		if got := RenderInstant(v); got != "" {
			t.Errorf("RenderInstant(%v) = %q, want empty", v, got)
		}
	}
}

func TestHumanSince(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		offset time.Duration
		want   string
	}{
		{-30 * time.Second, "just now"},
		{-5 * time.Minute, "5m ago"},
		{-3 * time.Hour, "3h ago"},
		{-50 * time.Hour, "2d ago"},
		{2 * time.Hour, "in 2h"},
	} {
		if got := humanSinceAt(reference.Add(tc.offset), reference); got != tc.want {
			t.Errorf("humanSinceAt(%v) = %q, want %q", tc.offset, got, tc.want)
		}
	}
}

func nan() float64 { return zero() / zero() }
func inf() float64 { return 1 / zero() }
func zero() float64 {
	var z float64
	return z
}
