// Package timeutil converts human-friendly time expressions into the instants
// and durations the Prometheus HTTP API expects, and renders the instants
// Prometheus returns back into forms an agent can read directly.
//
// This is deliberately the CLI's job, not the agent's. Prometheus takes
// timestamps as RFC3339 or fractional Unix seconds and returns them as
// fractional Unix seconds inside `[timestamp, "value"]` tuples; hand-building
// and hand-decoding those is the single most error-prone part of calling the
// query API, so the CLI owns it and accepts forgiving inputs (--since 1h,
// RFC3339, bare dates, epoch seconds/millis/micros and now±duration).
//
// This package backs prometheus-cli and is also importable as a library; see
// the repository README. Its rendered timestamp formats are consumed by agents
// parsing CLI output — keep them stable and extend additively.
package timeutil

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Range is an unresolved time window described by flags. Exactly one of Since
// or (From/To) is normally provided. Now, when zero, defaults to time.Now() —
// tests set it for determinism.
type Range struct {
	Since string
	From  string
	To    string
	Now   time.Time
}

// Resolve turns the Range into start/end instants. The window is validated to
// be non-empty and correctly ordered.
func (r Range) Resolve() (start, end time.Time, err error) {
	now := r.Now
	if now.IsZero() {
		now = time.Now()
	}

	switch {
	case r.Since != "":
		d, derr := ParseFlexDuration(r.Since)
		if derr != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --since %q: %w", r.Since, derr)
		}
		if d <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --since %q: must be a positive duration", r.Since)
		}
		end = now
		start = now.Add(-d)
	case r.From != "":
		start, err = ParseInstant(r.From, now)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --from %q: %w", r.From, err)
		}
		if r.To != "" {
			end, err = ParseInstant(r.To, now)
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("invalid --to %q: %w", r.To, err)
			}
		} else {
			end = now
		}
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("no time range given: pass --since (e.g. 1h) or --from/--to")
	}

	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("empty time range: end (%s) must be after start (%s)",
			end.Format(time.RFC3339), start.Format(time.RFC3339))
	}
	return start, end, nil
}

// Optional reports whether the Range carries any bound at all. The series,
// labels and metadata endpoints treat the window as optional, so a caller can
// distinguish "no window requested" from "an invalid window".
func (r Range) Optional() bool {
	return r.Since != "" || r.From != "" || r.To != ""
}

// FormatInstant renders an instant the way the Prometheus API takes it: Unix
// seconds, keeping sub-second precision only when it is present.
func FormatInstant(t time.Time) string {
	return FormatSeconds(float64(t.UnixNano()) / 1e9)
}

// FormatSeconds renders fractional Unix seconds without an exponent or padding.
func FormatSeconds(s float64) string {
	return strconv.FormatFloat(s, 'f', -1, 64)
}

// FromSeconds converts the fractional Unix seconds Prometheus returns inside a
// sample tuple into a UTC time.
func FromSeconds(s float64) time.Time {
	if math.IsNaN(s) || math.IsInf(s, 0) {
		return time.Time{}
	}
	sec, frac := math.Modf(s)
	return time.Unix(int64(sec), int64(math.Round(frac*1e9))).UTC()
}

// RenderInstant renders fractional Unix seconds as an RFC3339 string, or "" for
// a value that is not a real instant.
func RenderInstant(s float64) string {
	t := FromSeconds(s)
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

var flexDurationRe = regexp.MustCompile(`^(\d+)\s*([smhdwy])$`)

// ParseFlexDuration parses a duration string. In addition to Go's native units
// (ns, us, ms, s, m, h) it understands the Prometheus units d (days), w (weeks)
// and y (years), e.g. "15m", "2h", "7d", "1y".
func ParseFlexDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if m := flexDurationRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "d":
			return time.Duration(n) * 24 * time.Hour, nil
		case "w":
			return time.Duration(n) * 7 * 24 * time.Hour, nil
		case "y":
			return time.Duration(n) * 365 * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

var nowExprRe = regexp.MustCompile(`^now\s*([+-])\s*(.+)$`)

// ParseInstant parses a single point in time relative to now. It accepts:
//   - "now"                       → now
//   - "now-1h" / "now+30m"        → now ± duration (supports d/w/y units)
//   - a bare duration "1h" / "2d" → that long ago (now - duration)
//   - RFC3339 (with or without sub-second precision)
//   - "2006-01-02" (UTC midnight) and "2006-01-02 15:04:05" (UTC)
//   - an epoch in seconds (fractional allowed), milliseconds, microseconds or
//     nanoseconds, auto-detected by magnitude
func ParseInstant(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if strings.EqualFold(s, "now") {
		return now, nil
	}
	if m := nowExprRe.FindStringSubmatch(s); m != nil {
		d, err := ParseFlexDuration(m[2])
		if err != nil {
			return time.Time{}, err
		}
		if m[1] == "-" {
			return now.Add(-d), nil
		}
		return now.Add(d), nil
	}
	// Bare duration → that long ago.
	if d, err := ParseFlexDuration(s); err == nil {
		return now.Add(-d), nil
	}
	// Absolute layouts.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.UTC); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.UTC); err == nil {
		return t, nil
	}
	// Integer epoch, magnitude-detected.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return epochToTime(n), nil
	}
	// Fractional Unix seconds, the form Prometheus itself emits.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return FromSeconds(f), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time format (try RFC3339, 2006-01-02, an epoch, or now-1h)")
}

// epochToTime interprets an integer epoch whose unit is inferred from its
// magnitude: seconds (~1e9), milliseconds (~1e12), microseconds (~1e15) or
// nanoseconds (~1e18).
func epochToTime(n int64) time.Time {
	switch {
	case n < 1e12: // seconds
		return time.Unix(n, 0).UTC()
	case n < 1e15: // milliseconds
		return time.UnixMilli(n).UTC()
	case n < 1e18: // microseconds
		return time.UnixMicro(n).UTC()
	default: // nanoseconds
		return time.Unix(0, n).UTC()
	}
}

// HumanSince renders how long ago t was, relative to now, as a compact phrase:
// "just now", "5m ago", "3h ago", "2d ago" (or "in …" for future instants).
func HumanSince(t time.Time) string { return humanSinceAt(t, time.Now()) }

// humanSinceAt is HumanSince with an injectable "now" for deterministic tests.
func humanSinceAt(t, now time.Time) string {
	d := now.Sub(t)
	future := false
	if d < 0 {
		future, d = true, -d
	}
	if d < time.Minute {
		if future {
			return "in a moment"
		}
		return "just now"
	}
	var phrase string
	switch {
	case d < time.Hour:
		phrase = fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		phrase = fmt.Sprintf("%dh", int(d.Hours()))
	default:
		phrase = fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if future {
		return "in " + phrase
	}
	return phrase + " ago"
}
