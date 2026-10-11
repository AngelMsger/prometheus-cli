package auth

import (
	"errors"
	"testing"

	"github.com/angelmsger/agent-cli-kit/credstore"
	"github.com/angelmsger/prometheus-cli/internal/config"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/zalando/go-keyring"
)

// failingKeyring is a keychain backend whose every call fails with err.
type failingKeyring struct{ err error }

func (f failingKeyring) Get(string, string) (string, error) { return "", f.err }
func (f failingKeyring) Set(string, string, string) error   { return f.err }
func (f failingKeyring) Delete(string, string) error        { return f.err }

// newStoreWithKeyring returns prometheus-cli's Store over a substitute keychain
// backend. The store's own behavior is tested in agent-cli-kit/credstore.
func newStoreWithKeyring(dir string, backend credstore.Backend) *Store {
	return credstore.NewStoreWithBackend(dir, constants.KeychainService, backend)
}

func TestResolveCredentialRecovery(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		BaseURL: "https://prometheus.example.com",
		Auth:    config.AuthConfig{Scheme: SchemeBearer},
	}
	tests := []struct {
		name string
		err  error
		code string
		msg  string
		last string
	}{
		{"missing", keyring.ErrNotFound, "CREDENTIAL_NOT_VISIBLE_OR_MISSING",
			"stored Prometheus credentials are missing or not visible in this execution environment",
			"Only if the host retry also reports missing credentials, run `prometheus-cli config init` in the user's terminal or set PROMETHEUS_* environment variables."},
		{"inaccessible", errors.New("sandbox denied keychain"), "CREDENTIAL_STORE_INACCESSIBLE",
			"stored Prometheus credentials cannot be read in this execution environment",
			"Do not run `config init` unless the same check also fails in the host environment."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStoreWithKeyring(t.TempDir(), failingKeyring{err: tc.err})
			_, err := Resolve(cfg, config.Secrets{}, s)
			ce := cerrors.AsCLIError(err)
			if ce == nil || ce.Code != tc.code || ce.Message != tc.msg {
				t.Fatalf("Resolve() error = %+v, want %s %q", ce, tc.code, tc.msg)
			}
			if ce.Recovery == nil || ce.Recovery.Scope != "host" || ce.Retryable {
				t.Fatalf("Resolve() recovery = %+v retryable=%v, want host and false", ce.Recovery, ce.Retryable)
			}
			if len(ce.NextSteps) < 3 || ce.NextSteps[1] != "prometheus-cli doctor" || ce.NextSteps[2] != tc.last {
				t.Fatalf("Resolve() next steps = %q", ce.NextSteps)
			}
		})
	}
}

// With no scheme configured, prometheus-cli resolves `none` without touching
// the keychain, and the tenant still reaches the credential.
func TestResolveDefaultsToNoneAndCarriesTheTenant(t *testing.T) {
	t.Parallel()
	s := newStoreWithKeyring(t.TempDir(), failingKeyring{err: errors.New("must not be read")})
	cfg := config.Config{BaseURL: "https://prometheus.example.com", Auth: config.AuthConfig{TenantID: "team-a"}}
	cred, err := Resolve(cfg, config.Secrets{}, s)
	if err != nil || cred != (Credential{Scheme: SchemeNone, TenantID: "team-a"}) {
		t.Fatalf("Resolve() = %+v, %v", cred, err)
	}

	cfg.Auth.Scheme = SchemeBearer
	cred, err = Resolve(cfg, config.Secrets{Token: "tok"}, s)
	if err != nil || cred != (Credential{Scheme: SchemeBearer, Secret: "tok", TenantID: "team-a"}) {
		t.Fatalf("Resolve(bearer) = %+v, %v", cred, err)
	}
}
