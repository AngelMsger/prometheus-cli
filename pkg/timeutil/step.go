package timeutil

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// stepLadder is the set of round resolutions an auto-derived step snaps to, so
// a derived step reads like one a human would have typed ("1m", "5m", "1h")
// rather than "17.3s".
var stepLadder = []time.Duration{
	time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour,
}

// Step describes the resolution a range query will actually be evaluated at,
// together with how it was chosen. It is reported back to the caller so an
// agent always knows the resolution behind the numbers it is reading.
type Step struct {
	// Value is the resolution sent to Prometheus.
	Value time.Duration
	// Source is "flag" when the caller passed --step, "derived" when the CLI
	// chose one for the window, and "clamped" when the caller's step would
	// have exceeded Prometheus' maximum point count and was raised.
	Source string
	// Points is how many samples per series the query will resolve to.
	Points int
}

// String renders the step in the Prometheus duration form ("30s", "5m", "1h").
func (s Step) String() string { return FormatDuration(s.Value) }

// MarshalText renders the step as its Prometheus duration string, so JSON
// output carries "5m" rather than a nanosecond integer.
func (s Step) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// ResolveStep decides the resolution for a range query.
//
// Prometheus rejects a range query that would resolve to more than maxPoints
// samples per series ("exceeded maximum resolution of 11,000 points"), which is
// an error class an agent can only discover by hitting it — and then has to fix
// by arithmetic over a window it did not choose. So the CLI owns the step: with
// no --step it derives a round resolution near targetPoints samples, and with
// an explicit --step that would overrun the limit it raises the step and says
// so, instead of forwarding a request that is guaranteed to fail. This is a
// deliberate, documented difference from the sibling openobserve-cli, whose
// backend has no such ceiling and therefore requires --step outright.
func ResolveStep(start, end time.Time, requested string, targetPoints, maxPoints int) (Step, error) {
	span := end.Sub(start)
	if span <= 0 {
		return Step{}, fmt.Errorf("empty time range: end must be after start")
	}
	if targetPoints <= 0 {
		targetPoints = 250
	}
	if maxPoints <= 0 {
		maxPoints = 11000
	}

	if requested != "" {
		d, err := ParseStep(requested)
		if err != nil {
			return Step{}, err
		}
		if pointCount(span, d) <= maxPoints {
			return Step{Value: d, Source: "flag", Points: pointCount(span, d)}, nil
		}
		floor := minimumStep(span, maxPoints)
		clamped := snapUp(floor)
		return Step{Value: clamped, Source: "clamped", Points: pointCount(span, clamped)}, nil
	}

	ideal := span / time.Duration(targetPoints)
	derived := snapUp(ideal)
	if floor := snapUp(minimumStep(span, maxPoints)); derived < floor {
		derived = floor
	}
	return Step{Value: derived, Source: "derived", Points: pointCount(span, derived)}, nil
}

// ParseStep parses a --step value. It accepts the Prometheus duration units
// (including d/w/y) as well as a bare number of seconds, which is what the
// query_range endpoint itself takes.
func ParseStep(s string) (time.Duration, error) {
	d, err := ParseFlexDuration(s)
	if err != nil {
		if secs, ferr := parseSeconds(s); ferr == nil {
			d, err = secs, nil
		}
	}
	if err != nil {
		return 0, fmt.Errorf("invalid step %q: use a duration such as 30s, 1m, 5m or 1h", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("invalid step %q: must be a positive duration", s)
	}
	return d, nil
}

// FormatDuration renders a duration the way Prometheus writes one, preferring
// the largest whole unit ("2h" rather than "2h0m0s").
func FormatDuration(d time.Duration) string {
	switch {
	case d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", int64(d/(24*time.Hour)))
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(d/time.Hour))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(d/time.Minute))
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", int64(d/time.Second))
	default:
		return d.String()
	}
}

// minimumStep is the smallest step that keeps the window within maxPoints.
func minimumStep(span time.Duration, maxPoints int) time.Duration {
	step := span / time.Duration(maxPoints)
	if step <= 0 {
		step = time.Second
	}
	return step
}

// snapUp rounds a duration up to the next entry on the ladder, or to a whole
// number of weeks beyond it.
func snapUp(d time.Duration) time.Duration {
	if d < time.Second {
		return time.Second
	}
	for _, candidate := range stepLadder {
		if candidate >= d {
			return candidate
		}
	}
	week := 7 * 24 * time.Hour
	weeks := (d + week - 1) / week
	return weeks * week
}

// pointCount is how many samples per series a window resolves to at a step.
// Prometheus evaluates at start and at every step up to end, inclusive.
func pointCount(span, step time.Duration) int {
	if step <= 0 {
		return 0
	}
	return int(span/step) + 1
}

// parseSeconds accepts a bare number of seconds, the raw form the query_range
// endpoint takes. The whole string must parse, so "5x" is rejected rather than
// silently read as 5.
func parseSeconds(s string) (time.Duration, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(f * float64(time.Second)), nil
}
