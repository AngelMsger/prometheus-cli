package app

import (
	"errors"

	kitauth "github.com/angelmsger/agent-cli-kit/auth"

	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// credentialPersistenceFailure keeps storage recovery separate from acquiring
// another credential. A wrapped cause is retained, but never printed as a secret.
func credentialPersistenceFailure(err error, code, message string) *cerrors.CLIError {
	return cerrors.Wrap(err, cerrors.CategoryConfig, code, message).
		WithHint("Restore access to the host credential store and configuration directory before retrying; do not issue a new credential to fix a storage failure.").
		WithNextSteps("Retry the same command with access to the host user environment (home directory and OS keychain).", constants.AppName+" doctor").
		WithRecovery(kitauth.HostRecovery())
}

func logoutFailure(err error) error {
	var cliError *cerrors.CLIError
	if errors.As(err, &cliError) {
		return err // Preserve service-identity guards before any store access.
	}
	return credentialPersistenceFailure(err, "LOGOUT_FAILED", "stored credential removal did not complete; it may still be available")
}
