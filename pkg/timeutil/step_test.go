package timeutil

import (
	"testing"
	"time"
)

// The whole point of deriving a step is that no window an agent asks for can
// produce a request Prometheus refuses for exceeding its point ceiling.
func TestResolveStepNeverExceedsThePointCeiling(t *testing.T) {
	t.Parallel()
	end := reference
	for _, span := range []time.Duration{
		time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour,
		24 * time.Hour, 7 * 24 * time.Hour, 90 * 24 * time.Hour, 730 * 24 * time.Hour,
	} {
		step, err := ResolveStep(end.Add(-span), end, "", 250, 11000)
		if err != nil {
			t.Fatalf("span %s: %v", span, err)
		}
		if step.Points > 11000 {
			t.Errorf("span %s: derived step %s yields %d points", span, step, step.Points)
		}
		if step.Source != "derived" {
			t.Errorf("span %s: source = %q, want derived", span, step.Source)
		}
	}
}

func TestResolveStepDerivesRoundResolutions(t *testing.T) {
	t.Parallel()
	end := reference
	for _, tc := range []struct {
		span time.Duration
		want string
	}{
		{time.Hour, "15s"},
		{6 * time.Hour, "2m"},
		{24 * time.Hour, "10m"},
		{7 * 24 * time.Hour, "1h"},
	} {
		step, err := ResolveStep(end.Add(-tc.span), end, "", 250, 11000)
		if err != nil {
			t.Fatal(err)
		}
		if step.String() != tc.want {
			t.Errorf("span %s: step = %s, want %s", tc.span, step, tc.want)
		}
	}
}

func TestResolveStepKeepsAnExplicitStepThatFits(t *testing.T) {
	t.Parallel()
	step, err := ResolveStep(reference.Add(-time.Hour), reference, "5m", 250, 11000)
	if err != nil {
		t.Fatal(err)
	}
	if step.Source != "flag" || step.String() != "5m" || step.Points != 13 {
		t.Fatalf("got %+v", step)
	}
}

// An explicit step that would overrun the ceiling is raised rather than sent:
// forwarding it guarantees a server error the caller cannot act on.
func TestResolveStepClampsAnOversizedRequest(t *testing.T) {
	t.Parallel()
	step, err := ResolveStep(reference.Add(-30*24*time.Hour), reference, "1s", 250, 11000)
	if err != nil {
		t.Fatal(err)
	}
	if step.Source != "clamped" {
		t.Fatalf("source = %q, want clamped", step.Source)
	}
	if step.Points > 11000 {
		t.Fatalf("clamped step still yields %d points", step.Points)
	}
}

func TestResolveStepRejectsUnusableInput(t *testing.T) {
	t.Parallel()
	if _, err := ResolveStep(reference, reference, "", 250, 11000); err == nil {
		t.Error("accepted an empty window")
	}
	for _, bad := range []string{"5x", "-1m", "0s", "abc"} {
		if _, err := ResolveStep(reference.Add(-time.Hour), reference, bad, 250, 11000); err == nil {
			t.Errorf("accepted step %q", bad)
		}
	}
}

// The raw number of seconds is what the query_range endpoint itself takes, so
// a caller copying from the API docs must not be rejected.
func TestParseStepAcceptsBareSeconds(t *testing.T) {
	t.Parallel()
	got, err := ParseStep("30")
	if err != nil || got != 30*time.Second {
		t.Fatalf("ParseStep(\"30\") = %v, %v", got, err)
	}
}

func TestFormatDurationPrefersWholeUnits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{2 * time.Hour, "2h"},
		{48 * time.Hour, "2d"},
		{90 * time.Second, "90s"},
	} {
		if got := FormatDuration(tc.in); got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
