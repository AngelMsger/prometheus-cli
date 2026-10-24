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

// A context a stored login could serve lists `auth reuse` after the host retry
// and before any sign-in step, and first when it lacks its identity.
func TestResolveLeadsSignInWithReuse(t *testing.T) {
	t.Parallel()
	const step = "prometheus-cli --use-context 'team' auth reuse"
	s := newStoreWithKeyring(t.TempDir(), failingKeyring{err: keyring.ErrNotFound})
	cfg := config.Config{BaseURL: "https://prometheus.example.com", ReuseStep: step,
		Auth: config.AuthConfig{Scheme: SchemeBearer, TenantID: "team-a"}}
	_, l, err := ResolveLookup(cfg, config.Secrets{}, s)
	ce := cerrors.AsCLIError(err)
	if ce.Code != "CREDENTIAL_NOT_VISIBLE_OR_MISSING" || len(ce.NextSteps) < 4 ||
		ce.NextSteps[2] != step || ce.NextSteps[3] != MissingStep || l.Keychain != credstore.Miss {
		t.Fatalf("missing: %+v %+v", ce, l)
	}

	cfg.Auth.Scheme = SchemeBasic
	_, err = Resolve(cfg, config.Secrets{Password: "secret"}, s)
	if ce = cerrors.AsCLIError(err); ce.Code != "AUTH_NO_BASIC" || len(ce.NextSteps) == 0 || ce.NextSteps[0] != step {
		t.Fatalf("no identity: %+v", ce)
	}

	// An empty bearer secret reports AUTH_NO_TOKEN; it leads with reuse too.
	bearer := newStoreWithKeyring(t.TempDir(), failingKeyring{err: keyring.ErrNotFound})
	if _, err := bearer.Save(AccountKey(cfg.BaseURL, SchemeBearer), ""); err != nil {
		t.Fatal(err)
	}
	bcfg := cfg
	bcfg.Auth.Scheme = SchemeBearer
	_, err = Resolve(bcfg, config.Secrets{}, bearer)
	if ce = cerrors.AsCLIError(err); ce.Code != "AUTH_NO_TOKEN" || len(ce.NextSteps) == 0 || ce.NextSteps[0] != step {
		t.Fatalf("empty bearer: %+v", ce)
	}

	cfg.ReuseStep = ""
	_, err = Resolve(cfg, config.Secrets{Password: "secret"}, s)
	if ce = cerrors.AsCLIError(err); ce.Code != "AUTH_NO_BASIC" || (len(ce.NextSteps) > 0 && ce.NextSteps[0] == step) {
		t.Fatalf("offered reuse without a source: %+v", ce)
	}
}
