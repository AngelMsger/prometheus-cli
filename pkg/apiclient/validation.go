package apiclient

import cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"

// validateLimit rejects a mistaken negative bound before it can become an
// omitted parameter (and therefore an unbounded request).
func validateLimit(name string, value int) error {
	if value >= 0 {
		return nil
	}
	return cerrors.Newf(cerrors.CategoryUsage, "BAD_LIMIT", "--%s must be nonnegative", name).
		WithHint("Use a positive limit; 0 explicitly selects the endpoint's default or unlimited behavior.").
		WithNextSteps("prometheus-cli --help")
}
