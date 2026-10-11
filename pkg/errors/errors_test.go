package errors

import "testing"

// The model is tested in agent-cli-kit; this checks that prometheus-cli's own
// guidance is what an error built through this package carries.
func TestErrorsCarryPrometheusGuidance(t *testing.T) {
	e := New(CategoryConfig, "NO_BASE_URL", "no Prometheus server URL configured")
	if len(e.NextSteps) == 0 || e.NextSteps[0] != "prometheus-cli config init" {
		t.Fatalf("NextSteps = %v, want prometheus-cli's config guidance", e.NextSteps)
	}
	if ExitCode(e) != ExitConfig {
		t.Fatalf("ExitCode = %d, want %d", ExitCode(e), ExitConfig)
	}
}
