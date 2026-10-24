package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/angelmsger/agent-cli-kit/credstore"
	"github.com/angelmsger/prometheus-cli/internal/auth"
	"github.com/angelmsger/prometheus-cli/internal/config"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"
)

// The decision itself is agent-cli-kit/reuse's and tested there. These tests
// cover prometheus-cli's wiring: how contexts are described (complete URL with
// its path, plus the tenant), how the destination is rewritten, `none`, and the
// native store, verification and command.

const reuseBase = "https://service.example.test/deploy"

// fakeStore answers lookups from keychain entries keyed like the real store.
type fakeStore map[string]string

func (f fakeStore) lookup(key string) credstore.Lookup {
	if v, ok := f[key]; ok {
		return credstore.Lookup{Secret: v, Keychain: credstore.Hit}
	}
	return credstore.Lookup{Keychain: credstore.Miss, File: credstore.Miss}
}

func (f fakeStore) resolve(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
	l := f.lookup(auth.AccountKey(cfg.BaseURL, cfg.Auth.Scheme))
	if !l.Found() {
		return auth.Credential{}, l, cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_NOT_VISIBLE_OR_MISSING", "missing")
	}
	cred := auth.Credential{Scheme: cfg.Auth.Scheme, Username: cfg.Auth.Username, Secret: l.Secret, TenantID: cfg.Auth.TenantID}
	return cred, l, cred.Validate()
}

func rejected() error {
	return cerrors.New(cerrors.CategoryAuth, "HTTP_UNAUTHORIZED", "rejected").WithHTTPStatus(401)
}

// reuseFixture writes contexts, the last one being the destination; by default
// a personal basic login and a team basic context without a username. The
// store holds the personal basic secret, and verification accepts it for
// existing-user only.
func reuseFixture(t *testing.T, contexts ...config.NamedContext) (*appState, fakeStore, reuseServices) {
	t.Helper()
	dir := t.TempDir()
	if len(contexts) == 0 {
		contexts = []config.NamedContext{
			{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
			{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic"}},
		}
	}
	file := config.File{CurrentContext: contexts[0].Name, Contexts: contexts}
	if err := config.WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	target := contexts[len(contexts)-1]
	s := &appState{cfgDir: dir, resolved: &config.Resolved{Config: config.StoredContext(target, file.Defaults), ActiveContext: target.Name}}
	store := fakeStore{auth.AccountKey(reuseBase, "basic"): "test-secret"}
	services := reuseServices{Read: config.ReadFile, Write: config.WriteFile, Resolve: store.resolve, Lookup: store.lookup,
		Verify: func(_ context.Context, _ config.Config, cred auth.Credential) error {
			if cred.Secret == "test-secret" && cred.Username == "existing-user" {
				return nil
			}
			return rejected()
		}}
	return s, store, services
}

func targetOf(t *testing.T, s *appState) config.NamedContext {
	t.Helper()
	file, _, err := config.ReadFile(s.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := file.Context(s.resolved.ActiveContext)
	return c
}

func TestAuthReusePreviewApplyAndIdempotence(t *testing.T) {
	s, _, services := reuseFixture(t)
	path := config.ConfigFilePath(s.cfgDir)
	bytesBefore, _ := os.ReadFile(path)
	before, _, _ := config.ReadFile(s.cfgDir)
	preview, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err != nil || !preview.Changed || preview.State != "available" || !preview.Verified || preview.Scheme != "basic" {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if afterPreview, _ := os.ReadFile(path); string(afterPreview) != string(bytesBefore) {
		t.Fatal("preview changed config")
	}
	applied, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || applied.State != "reused" || applied.SourceContext != "personal" || applied.Username != "existing-user" {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	if got := targetOf(t, s); got.Auth.Username != "existing-user" || after.CurrentContext != before.CurrentContext || !reflect.DeepEqual(after.Contexts[0], before.Contexts[0]) {
		t.Fatalf("did not preserve the source or the current context: %+v", after)
	}
	services.Write = func(string, config.File) error { t.Fatal("repeated write"); return nil }
	services.Resolve = func(config.Config) (auth.Credential, credstore.Lookup, error) {
		t.Fatal("populated identity must remain unchanged")
		return auth.Credential{}, credstore.Lookup{}, nil
	}
	repeated, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || repeated.Changed || repeated.State != "unchanged" {
		t.Fatalf("repeat: %+v %v", repeated, err)
	}
	raw, _ := json.Marshal(applied)
	if strings.Contains(string(raw), "test-secret") {
		t.Fatal("secret reached output")
	}
	if len(applied.NextSteps) != 1 || !strings.HasSuffix(applied.NextSteps[0], "auth status") {
		t.Fatalf("next steps: %v", applied.NextSteps)
	}
}

// A bearer team context whose token is gone adopts the personal basic login on
// the same service and tenant.
func TestAuthReuseBearerContextAdoptsBasicLogin(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user", TenantID: "team-a"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer", TenantID: "team-a"}},
	)
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.State != "reused" || r.SourceContext != "personal" || r.Scheme != "basic" || r.Username != "existing-user" {
		t.Fatalf("got %+v %v", r, err)
	}
	if got := targetOf(t, s); got.Auth.Scheme != "basic" || got.Auth.Username != "existing-user" || got.Auth.TenantID != "team-a" {
		t.Fatalf("destination %+v", got)
	}
}

// A bearer context whose username is only a label adopts the basic secret
// stored for its service, which no context points at any more.
func TestAuthReuseLabelledBearerAdoptsStoredSecret(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer", Username: "existing-user"}},
	)
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.State != "reused" || r.SourceContext != "" || r.Scheme != "basic" || r.Username != "existing-user" {
		t.Fatalf("got %+v %v", r, err)
	}
	if got := targetOf(t, s); got.Auth.Scheme != "basic" || got.Auth.Username != "existing-user" {
		t.Fatalf("destination %+v", got)
	}
}

// Keys include the path, so a secret stored for another deployment on the same
// host is never offered to this one.
func TestAuthReuseStoredSecretFollowsThePath(t *testing.T) {
	s, store, services := reuseFixture(t,
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer", Username: "existing-user"}},
	)
	delete(store, auth.AccountKey(reuseBase, "basic"))
	store[auth.AccountKey("https://service.example.test/other", "basic")] = "test-secret"
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.Changed || r.State != "unchanged" {
		t.Fatalf("another path's secret was adopted: %+v %v", r, err)
	}
}

// A working login is kept, even when another scheme is selected explicitly:
// only a browser session gives way to an explicit choice, and Prometheus has
// none.
func TestAuthReuseKeepsAWorkingLoginEvenWhenSelected(t *testing.T) {
	s, store, services := reuseFixture(t,
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer", Username: "existing-user"}},
	)
	store[auth.AccountKey(reuseBase, "bearer")] = "team-token"
	verify := services.Verify
	services.Verify = func(ctx context.Context, cfg config.Config, cred auth.Credential) error {
		if cred.Scheme == "bearer" && cred.Secret == "team-token" {
			return nil
		}
		return verify(ctx, cfg, cred)
	}
	services.Write = func(string, config.File) error { t.Fatal("replaced a working login"); return nil }
	for _, opts := range []reuseOptions{{}, {FromScheme: "basic"}} {
		r, err := reuseAuthentication(s, opts, services)
		if err != nil || r.State != "unchanged" || !r.Verified || r.Changed {
			t.Fatalf("%+v: %+v %v", opts, r, err)
		}
	}
}

func TestAuthReuseMatchesCompleteServiceBeforeCredentialAccess(t *testing.T) {
	for _, kind := range []string{"host", "path", "protocol", "port", "tenant"} {
		t.Run(kind, func(t *testing.T) {
			personal := config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}}
			switch kind {
			case "host":
				personal.BaseURL = "https://other.example.test/deploy"
			case "path":
				personal.BaseURL = "https://service.example.test/other"
			case "protocol":
				personal.BaseURL = "http://service.example.test/deploy"
			case "port":
				personal.BaseURL = "https://service.example.test:8443/deploy"
			case "tenant":
				personal.Auth.TenantID = "other-tenant"
			}
			s, _, services := reuseFixture(t, personal, config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic"}})
			resolve := services.Resolve
			services.Resolve = func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
				if cfg.Auth.Username == "existing-user" {
					t.Fatal("read another service's credential")
				}
				return resolve(cfg)
			}
			result, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
			if err != nil || result.Changed || result.State != "unavailable" {
				t.Fatalf("mismatch: %+v %v", result, err)
			}
			_, err = reuseAuthentication(s, reuseOptions{From: "personal", DryRun: true}, services)
			if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_SOURCE_MISMATCH" {
				t.Fatalf("explicit mismatch: %v", err)
			}
		})
	}
}

// A scheme the invocation supplies keeps reuse within that scheme.
func TestAuthReuseSuppliedSchemeStaysWithinIt(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer"}},
	)
	s.resolved.Sources = map[string]string{config.FieldAuthScheme: "flag"}
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.State != "unavailable" || r.Changed {
		t.Fatalf("pinned: %+v %v", r, err)
	}
	for _, opts := range []reuseOptions{{From: "personal"}, {FromScheme: "basic"}} {
		_, err = reuseAuthentication(s, opts, services)
		if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_SOURCE_MISMATCH" {
			t.Fatalf("pinned explicit %+v: %v", opts, err)
		}
	}
}

func TestAuthReusePreservesDestinationIdentity(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "other-user"}},
	)
	services.Resolve = func(config.Config) (auth.Credential, credstore.Lookup, error) {
		t.Fatal("must not replace an existing identity")
		return auth.Credential{}, credstore.Lookup{}, nil
	}
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.State != "unchanged" || r.Changed {
		t.Fatalf("existing identity: %+v %v", r, err)
	}
}

func TestAuthReuseAmbiguityAndExplicitSelection(t *testing.T) {
	second := "https://SERVICE.example.test:443/deploy"
	s, store, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer"}},
		config.NamedContext{Name: "second", BaseURL: second, Auth: config.AuthConfig{Scheme: "bearer"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic"}},
	)
	store[auth.AccountKey(reuseBase, "bearer")] = "first"
	store[auth.AccountKey(second, "bearer")] = "second"
	delete(store, auth.AccountKey(reuseBase, "basic"))
	services.Verify = func(context.Context, config.Config, auth.Credential) error { return nil }
	_, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_AMBIGUOUS" {
		t.Fatalf("ambiguity: %v", err)
	}
	r, err := reuseAuthentication(s, reuseOptions{FromScheme: "bearer", DryRun: true}, services)
	if err != nil || r.State != "available" || r.SourceContext != "" || r.Scheme != "bearer" {
		t.Fatalf("stored secret: %+v %v", r, err)
	}
	r, err = reuseAuthentication(s, reuseOptions{From: "SECOND"}, services)
	if err != nil || r.SourceContext != "second" || r.State != "reused" {
		t.Fatalf("explicit choice: %+v %v", r, err)
	}
	if got := targetOf(t, s); got.BaseURL != second || got.Auth.Scheme != "bearer" {
		t.Fatalf("destination does not resolve the chosen key: %+v", got)
	}
}

// One stored basic secret pairs with every username the service records; when
// two pairings verify, --from-scheme with --from-context picks one.
func TestAuthReuseStoredSecretPairsWithSelectedContextUsername(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "alice", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer", Username: "existing-user"}},
		config.NamedContext{Name: "bob", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer", Username: "other-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer"}},
	)
	services.Verify = func(_ context.Context, _ config.Config, cred auth.Credential) error {
		if cred.Scheme == "basic" && cred.Secret == "test-secret" {
			return nil
		}
		return rejected()
	}
	_, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	ce := cerrors.AsCLIError(err)
	if err == nil || ce.Code != "AUTH_REUSE_AMBIGUOUS" {
		t.Fatalf("ambiguity: %v", err)
	}
	if !strings.Contains(strings.Join(ce.NextSteps, "\n"), "--from-scheme basic") {
		t.Fatalf("next steps do not offer --from-scheme: %v", ce.NextSteps)
	}
	r, err := reuseAuthentication(s, reuseOptions{FromScheme: "basic", From: "alice"}, services)
	if err != nil || r.State != "reused" || r.Username != "existing-user" || r.SourceContext != "" {
		t.Fatalf("paired choice: %+v %v", r, err)
	}
	if got := targetOf(t, s); got.Auth.Scheme != "basic" || got.Auth.Username != "existing-user" {
		t.Fatalf("destination %+v", got)
	}
}

func TestAuthReuseDuplicateAliasesDoNotRequireChoice(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "alias", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
		config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic"}},
	)
	r, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err != nil || !r.Changed || r.State != "available" {
		t.Fatalf("same native credential: %+v %v", r, err)
	}
}

func TestAuthReuseRetainsOperationalFailures(t *testing.T) {
	for _, kind := range []string{"store", "network", "permission", "identity"} {
		t.Run(kind, func(t *testing.T) {
			s, _, services := reuseFixture(t)
			before, _, _ := config.ReadFile(s.cfgDir)
			failure := cerrors.New(cerrors.CategoryNetwork, "NETWORK_ERROR", "network unavailable")
			switch kind {
			case "store":
				failure = cerrors.New(cerrors.CategoryConfig, "CREDENTIAL_STORE_INACCESSIBLE", "host keychain unavailable")
				services.Resolve = func(config.Config) (auth.Credential, credstore.Lookup, error) {
					return auth.Credential{}, credstore.Lookup{Keychain: credstore.Failed}, failure
				}
			case "permission":
				failure = cerrors.New(cerrors.CategoryPermission, "FORBIDDEN", "access denied")
			case "identity":
				failure = cerrors.New(cerrors.CategoryAuth, "AUTH_IDENTITY_UNAVAILABLE", "authenticated identity could not be established")
			}
			if kind != "store" {
				services.Verify = func(context.Context, config.Config, auth.Credential) error { return failure }
			}
			_, err := reuseAuthentication(s, reuseOptions{}, services)
			if !errors.Is(err, failure) {
				t.Fatalf("lost diagnostic: %v", err)
			}
			if after, _, _ := config.ReadFile(s.cfgDir); !reflect.DeepEqual(before, after) {
				t.Fatal("failure mutated config")
			}
		})
	}
}

func TestAuthReuseExpiredIdentityIsUnavailable(t *testing.T) {
	s, _, services := reuseFixture(t)
	services.Verify = func(context.Context, config.Config, auth.Credential) error { return rejected() }
	r, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err != nil || r.State != "unavailable" || r.Changed {
		t.Fatalf("expired credential: %+v %v", r, err)
	}
}

func TestAuthReuseSelfConfigurationAndConcurrentChange(t *testing.T) {
	s, _, services := reuseFixture(t)
	s.resolved.Config.Defaults.ReadOnly = true
	r, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err != nil || !r.Changed {
		t.Fatal("read-only preview blocked", err)
	}
	r, err = reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.State != "reused" {
		t.Fatalf("native self-configuration should remain available: %+v %v", r, err)
	}

	s, _, services = reuseFixture(t)
	verify := services.Verify
	services.Verify = func(ctx context.Context, cfg config.Config, cred auth.Credential) error {
		f, _, _ := config.ReadFile(s.cfgDir)
		f.CurrentContext = "changed-by-user"
		if err := config.WriteFile(s.cfgDir, f); err != nil {
			return err
		}
		return verify(ctx, cfg, cred)
	}
	_, err = reuseAuthentication(s, reuseOptions{}, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_CONFIG_CHANGED" {
		t.Fatalf("concurrent change: %v", err)
	}
}

func TestAuthReuseRejectsCredentialRotationBeforeAssociation(t *testing.T) {
	s, store, services := reuseFixture(t)
	before, _, _ := config.ReadFile(s.cfgDir)
	resolve := services.Resolve
	calls := 0
	services.Resolve = func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
		calls++
		if calls > 1 {
			store[auth.AccountKey(reuseBase, "basic")] = "rotated-secret"
		}
		return resolve(cfg)
	}
	_, err := reuseAuthentication(s, reuseOptions{}, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_CREDENTIAL_CHANGED" {
		t.Fatalf("credential rotation: %v", err)
	}
	if after, _, _ := config.ReadFile(s.cfgDir); !reflect.DeepEqual(before, after) {
		t.Fatal("rotation changed destination")
	}
}

func TestAuthReuseRejectsTargetOverridesAndUnknownSource(t *testing.T) {
	s, _, services := reuseFixture(t)
	s.resolved.Config.BaseURL = "https://service.example.test/other"
	_, err := reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_TARGET_MISMATCH" {
		t.Fatal(err)
	}
	s.resolved.Config.BaseURL = reuseBase
	s.resolved.Config.Auth.TenantID = "other-tenant"
	_, err = reuseAuthentication(s, reuseOptions{DryRun: true}, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_TARGET_MISMATCH" {
		t.Fatalf("tenant override: %v", err)
	}
	s.resolved.Config.Auth.TenantID = ""
	_, err = reuseAuthentication(s, reuseOptions{From: "missing", DryRun: true}, services)
	if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_SOURCE_NOT_FOUND" {
		t.Fatal(err)
	}
}

func TestAuthReuseUsesPersistedDefaultsWithoutEnvironmentIdentity(t *testing.T) {
	s, _, services := reuseFixture(t)
	s.resolved.Secrets = config.Secrets{Password: "environment-secret"}
	s.resolved.Config.Auth.Username = "environment-user"
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.State != "reused" {
		t.Fatalf("stored defaults: %+v %v", r, err)
	}
	if got := targetOf(t, s); got.Auth.Username != "existing-user" {
		t.Fatalf("identity did not come from the stored source: %+v", got)
	}
}

// `none` is neither a destination nor a source: an anonymous context reads no
// credential, and naming one as the source is refused rather than switching an
// authenticated context to no authentication.
func TestAuthReuseNoneIsNeitherDestinationNorSource(t *testing.T) {
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "none"}},
	)
	services.Resolve = func(config.Config) (auth.Credential, credstore.Lookup, error) {
		t.Fatal("anonymous service must not read credentials")
		return auth.Credential{}, credstore.Lookup{}, nil
	}
	for _, opts := range []reuseOptions{{}, {From: "personal"}, {FromScheme: "basic"}} {
		r, err := reuseAuthentication(s, opts, services)
		if err != nil || r.Changed || r.Verified || r.State != "unchanged" {
			t.Fatalf("anonymous destination %+v: %+v %v", opts, r, err)
		}
	}

	s, _, services = reuseFixture(t,
		config.NamedContext{Name: "anonymous", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "none"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic"}},
	)
	resolve := services.Resolve
	services.Resolve = func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
		if cfg.Auth.Scheme == "none" {
			t.Fatal("an anonymous context was treated as a source")
		}
		return resolve(cfg)
	}
	services.Verify = func(context.Context, config.Config, auth.Credential) error { return nil }
	services.Write = func(string, config.File) error { t.Fatal("adopted an anonymous source"); return nil }
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.Changed || r.State != "unavailable" {
		t.Fatalf("automatic: %+v %v", r, err)
	}
	for _, opts := range []reuseOptions{{From: "anonymous"}, {FromScheme: "none"}} {
		_, err = reuseAuthentication(s, opts, services)
		if err == nil || cerrors.AsCLIError(err).Code != "AUTH_REUSE_SOURCE_MISMATCH" {
			t.Fatalf("explicit anonymous source %+v: %v", opts, err)
		}
	}
}

// The command resolves through the native store and verifies through the
// native client, sending the destination's tenant with the reused credential.
func TestAuthReuseCommandVerifiesNativeStoredCredential(t *testing.T) {
	keyring.MockInit()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		user, password, ok := r.BasicAuth()
		if r.Method != http.MethodGet || !ok || user != "existing-user" || password != "retained-secret" || r.Header.Get("X-Scope-OrgID") != "team-a" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.53.0"}}`))
	}))
	defer server.Close()
	base := server.URL + "/deploy"
	s, _, _ := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: base, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user", TenantID: "team-a"}},
		config.NamedContext{Name: "team", BaseURL: base, Auth: config.AuthConfig{Scheme: "bearer", TenantID: "team-a"}},
	)
	s.store = auth.NewStore(s.cfgDir)
	credential := auth.Credential{Scheme: "basic", Username: "existing-user", Secret: "retained-secret"}
	if _, err := auth.Save(base, credential, s.store); err != nil {
		t.Fatal(err)
	}
	command := newAuthReuseCmd(s)
	if err := command.RunE(command, nil); err != nil {
		t.Fatal(err)
	}
	if requests == 0 {
		t.Fatal("did not verify with the native API client")
	}
	after, _, _ := config.ReadFile(s.cfgDir)
	target, _ := after.Context("team")
	actual, err := auth.Resolve(config.StoredContext(target, after.Defaults), config.Secrets{}, auth.NewStore(s.cfgDir))
	if err != nil || actual.Username != "existing-user" || actual.Secret != credential.Secret || actual.TenantID != "team-a" {
		t.Fatalf("native reuse unavailable after reload: %+v %v", actual, err)
	}
}

func TestAuthReusePreservesLegacyStoreKeyUnderEquivalentOverride(t *testing.T) {
	keyring.MockInit()
	legacy := "https://SERVICE.example.test:443/deploy/"
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: legacy, Auth: config.AuthConfig{Scheme: "basic", Username: "existing-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "basic"}},
	)
	store := auth.NewStore(s.cfgDir)
	credential := auth.Credential{Scheme: "basic", Username: "existing-user", Secret: "retained-secret"}
	if _, err := auth.Save(legacy, credential, store); err != nil {
		t.Fatal(err)
	}
	services.Resolve = func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
		return auth.ResolveLookup(cfg, config.Secrets{}, store)
	}
	services.Lookup = store.Lookup
	services.Verify = func(context.Context, config.Config, auth.Credential) error { return nil }
	if _, err := reuseAuthentication(s, reuseOptions{}, services); err != nil {
		t.Fatal(err)
	}
	fresh, err := config.Load(config.LoadOptions{ConfigDir: s.cfgDir, Context: "team", DotenvPath: filepath.Join(t.TempDir(), "absent"), Flags: config.FlagValues{BaseURL: reuseBase}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := auth.Resolve(fresh.Config, config.Secrets{}, auth.NewStore(s.cfgDir))
	if err != nil || got.Secret != credential.Secret || got.Username != credential.Username {
		t.Fatalf("equivalent override lost native credential: %v", err)
	}
	if fresh.Config.BaseURL != reuseBase {
		t.Fatal("changed request destination")
	}
	if err := auth.ForgetForConfig(fresh.Config, fresh.Config.Auth.Scheme, store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(auth.AccountKey(legacy, credential.Scheme)); err == nil {
		t.Fatal("logout left the original credential active")
	}
	fresh.Config.BaseURL = "https://service.example.test/other"
	_, err = auth.Resolve(fresh.Config, config.Secrets{}, store)
	if err == nil || cerrors.AsCLIError(err).Code != "CREDENTIAL_SERVICE_MISMATCH" {
		t.Fatalf("foreign deployment reused stored key: %v", err)
	}
}

func TestAuthReusePreservesWorkingTokenWithoutUsername(t *testing.T) {
	keyring.MockInit()
	legacy := "https://SERVICE.example.test:443/deploy/"
	s, _, services := reuseFixture(t,
		config.NamedContext{Name: "personal", BaseURL: legacy, Auth: config.AuthConfig{Scheme: "bearer", Username: "existing-user"}},
		config.NamedContext{Name: "team", BaseURL: reuseBase, Auth: config.AuthConfig{Scheme: "bearer"}},
	)
	before, _, _ := config.ReadFile(s.cfgDir)
	store := auth.NewStore(s.cfgDir)
	existing := auth.Credential{Scheme: "bearer", Secret: "destination-secret"}
	if _, err := auth.Save(reuseBase, existing, store); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Save(legacy, auth.Credential{Scheme: "bearer", Secret: "other-source-secret"}, store); err != nil {
		t.Fatal(err)
	}
	services.Resolve = func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
		return auth.ResolveLookup(cfg, config.Secrets{}, store)
	}
	services.Lookup = store.Lookup
	services.Verify = func(_ context.Context, _ config.Config, credential auth.Credential) error {
		if credential.Secret != existing.Secret {
			t.Fatal("attempted to switch an already valid destination login")
		}
		return nil
	}
	r, err := reuseAuthentication(s, reuseOptions{}, services)
	if err != nil || r.Changed || !r.Verified || r.State != "unchanged" {
		t.Fatalf("existing token: %+v %v", r, err)
	}
	if after, _, _ := config.ReadFile(s.cfgDir); !reflect.DeepEqual(before, after) {
		t.Fatal("modified a valid token-only context")
	}
}

// A rejected credential leads with reuse when a stored login could replace it.
func TestRejectedCredentialRecoveryLeadsWithReuse(t *testing.T) {
	s := &appState{resolved: &config.Resolved{Config: config.Config{BaseURL: reuseBase,
		Auth: config.AuthConfig{Scheme: "bearer"}, ReuseStep: "prometheus-cli --use-context 'team' auth reuse"}}}
	ce := rejected().(*cerrors.CLIError).WithNextSteps("prometheus-cli auth status", "prometheus-cli config init")
	withReuseRecovery(ce, s, &cobra.Command{Use: "query"})
	if len(ce.NextSteps) != 3 || ce.NextSteps[0] != s.cfg().ReuseStep {
		t.Fatalf("next steps %v", ce.NextSteps)
	}
	login := rejected().(*cerrors.CLIError)
	steps := append([]string(nil), login.NextSteps...)
	withReuseRecovery(login, s, &cobra.Command{Use: "login"})
	if !reflect.DeepEqual(steps, login.NextSteps) {
		t.Fatal("login gained a reuse step")
	}
	forbidden := cerrors.New(cerrors.CategoryPermission, "FORBIDDEN", "denied").WithHTTPStatus(403)
	steps = append([]string(nil), forbidden.NextSteps...)
	withReuseRecovery(forbidden, s, &cobra.Command{Use: "query"})
	if !reflect.DeepEqual(steps, forbidden.NextSteps) {
		t.Fatal("a permission error gained a reuse step")
	}
}
