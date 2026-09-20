package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// A dropped target was never scraped, so it is not "unhealthy" — reporting it
// as such would send an investigation after a target that does not exist.
func TestKeepUnhealthyExcludesDroppedTargets(t *testing.T) {
	t.Parallel()
	got := keepUnhealthy([]apiclient.Target{
		{State: "active", Health: "up", Instance: "a"},
		{State: "active", Health: "down", Instance: "b"},
		{State: "active", Health: "unknown", Instance: "c"},
		{State: "dropped", Instance: "d"},
	})
	if len(got) != 2 || got[0].Instance != "b" || got[1].Instance != "c" {
		t.Fatalf("got %+v", got)
	}
}

// A rule whose evaluation errored keeps serving its stale result, so health is
// the only signal that it is broken.
func TestKeepFailingCatchesBothSignals(t *testing.T) {
	t.Parallel()
	got := keepFailing([]apiclient.Rule{
		{Name: "ok", Health: "ok"},
		{Name: "errored", Health: "err"},
		{Name: "with-message", Health: "ok", LastError: "boom"},
	})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterAlerts(t *testing.T) {
	t.Parallel()
	alerts := []apiclient.Alert{
		{Name: "A", State: "firing"},
		{Name: "B", State: "pending"},
	}
	if got := filterAlerts(alerts, "", ""); len(got) != 2 {
		t.Fatalf("no filter should pass everything, got %+v", got)
	}
	if got := filterAlerts(alerts, "FIRING", ""); len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("state filter = %+v", got)
	}
	if got := filterAlerts(alerts, "", "b"); len(got) != 1 || got[0].Name != "B" {
		t.Fatalf("name filter should be case-insensitive, got %+v", got)
	}
}

// A destructive write must fail with instructions rather than block on a prompt
// no agent or CI run can answer.
func TestRequireConfirmationNeverPrompts(t *testing.T) {
	t.Parallel()
	if err := requireConfirmation(true, "delete series", "x"); err != nil {
		t.Fatalf("--yes should pass, got %v", err)
	}
	ce := cerrors.AsCLIError(requireConfirmation(false, "delete series", "erasing samples"))
	if ce.Code != "CONFIRMATION_REQUIRED" || cerrors.ExitCode(ce) != cerrors.ExitUsage {
		t.Fatalf("got %+v", ce)
	}
	if len(ce.NextSteps) != 2 {
		t.Fatalf("next steps = %v", ce.NextSteps)
	}
}

func TestReadInlineOrFile(t *testing.T) {
	t.Parallel()
	if got, err := readInlineOrFile("up == 0"); err != nil || got != "up == 0" {
		t.Fatalf("plain value = %q, %v", got, err)
	}
	// A literal leading @ is escapable, so a selector starting with one is not
	// mistaken for a file reference.
	if got, err := readInlineOrFile(`\@literal`); err != nil || got != "@literal" {
		t.Fatalf("escaped value = %q, %v", got, err)
	}

	path := filepath.Join(t.TempDir(), "q.promql")
	if err := os.WriteFile(path, []byte("  sum(up)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readInlineOrFile("@" + path); err != nil || got != "sum(up)" {
		t.Fatalf("file value = %q, %v", got, err)
	}
	if _, err := readInlineOrFile("@/no/such/file"); err == nil {
		t.Fatal("a missing query file should be an error")
	}
}

// An expression containing a quote is pasted back into a suggested command, so
// it has to stay valid inside single quotes.
func TestShellQuoteKeepsSuggestionsRunnable(t *testing.T) {
	t.Parallel()
	if got := shellQuote(`up{job="a"}`); got != `up{job="a"}` {
		t.Fatalf("double quotes should pass through: %q", got)
	}
	if got := shellQuote(`up{job='a'}`); got != `up{job='"'"'a'"'"'}` {
		t.Fatalf("single quotes were not escaped: %q", got)
	}
}
