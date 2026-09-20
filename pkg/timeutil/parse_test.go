package timeutil

import (
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
