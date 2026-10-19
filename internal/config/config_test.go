package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// TestFileFormat pins the bytes of config.yaml. The engine is shared; the
// shape is prometheus-cli's, and existing files must keep reading and writing
// the same way.
func TestFileFormat(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	in := File{
		CurrentContext: "mimir",
		Contexts: []NamedContext{
			{Name: "default", BaseURL: "http://localhost:9090", Auth: AuthConfig{Scheme: SchemeNone}},
			{Name: "mimir", BaseURL: "https://mimir.example.com/prometheus", Auth: AuthConfig{Scheme: SchemeBearer,
				CredentialURL: "https://grafana.example.com/tokens", TenantID: "team-a"}},
			{Name: "gateway", BaseURL: "https://gw.example.com", Auth: AuthConfig{Scheme: SchemeBasic, Username: "alice"}},
			{Name: "bare"},
		},
		Defaults: Defaults{Format: "table", Timeout: 90 * time.Second, MaxRetries: 5, ReadOnly: true},
	}
	if err := WriteFile(dir, in); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	const want = `current_context: mimir
contexts:
    - name: default
      server: http://localhost:9090
      auth:
        scheme: none
    - name: mimir
      server: https://mimir.example.com/prometheus
      auth:
        credential_url: https://grafana.example.com/tokens
        scheme: bearer
        tenant_id: team-a
    - name: gateway
      server: https://gw.example.com
      auth:
        scheme: basic
        username: alice
    - name: bare
defaults:
    format: table
    timeout: 1m30s
    max_retries: 5
    read_only: true
`
	if string(raw) != want {
		t.Errorf("config.yaml changed:\n%s", raw)
	}
	got, ok, err := ReadFile(dir)
	if err != nil || !ok || !reflect.DeepEqual(got, in) {
		t.Errorf("read back %+v, %v, %v", got, ok, err)
	}
}

// TestSchema pins prometheus-cli's environment variables, defaults, scheme
// inference and URL rules.
func TestSchema(t *testing.T) {
	cleanSetupEnv(t)
	dir := t.TempDir()
	load := func() *Resolved {
		t.Helper()
		r, err := Load(LoadOptions{ConfigDir: dir, DotenvPath: filepath.Join(dir, "absent.env")})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := load()
	want := Config{Auth: AuthConfig{Scheme: SchemeNone}, Defaults: Defaults{Format: "json", Timeout: 30 * time.Second, MaxRetries: 3}}
	if !reflect.DeepEqual(r.Config, want) || r.Sources[FieldAuthScheme] != "default" {
		t.Fatalf("defaults = %+v (%v)", r.Config, r.Sources)
	}

	t.Setenv("PROMETHEUS_URL", "prom.example.com:9090/api/v1/")
	t.Setenv("PROMETHEUS_USER", "alice")
	t.Setenv("PROMETHEUS_CREDENTIAL_URL", "https://help.example.com/token")
	t.Setenv("PROMETHEUS_TENANT", "team-a")
	t.Setenv("PROMETHEUS_FORMAT", "table")
	t.Setenv("PROMETHEUS_CLI_READ_ONLY", "yes")
	t.Setenv("PROMETHEUS_PASSWORD", "pw")
	r = load()
	if r.Config.BaseURL != "prom.example.com:9090/api/v1/" || r.Config.Auth.Username != "alice" || r.Config.Auth.Scheme != SchemeBasic ||
		r.Config.Auth.CredentialURL != "https://help.example.com/token" || r.Config.Auth.TenantID != "team-a" ||
		r.Config.Defaults.Format != "table" || !r.Config.Defaults.ReadOnly || r.Secrets != (Secrets{Password: "pw"}) ||
		r.Sources[FieldTenantID] != "env" {
		t.Fatalf("environment = %+v %+v", r.Config, r.Secrets)
	}
	t.Setenv("PROMETHEUS_TOKEN", "tok")
	if r = load(); r.Config.Auth.Scheme != SchemeBearer {
		t.Errorf("a bearer token wins over a password: %q", r.Config.Auth.Scheme)
	}
	t.Setenv("PROMETHEUS_AUTH_SCHEME", SchemeBasic)
	if r = load(); r.Config.Auth.Scheme != SchemeBasic {
		t.Errorf("an explicit scheme wins: %q", r.Config.Auth.Scheme)
	}
	// A bare host gets http://, and a pasted API root is trimmed.
	if base, err := NormalizeServiceURL(r.Config.BaseURL); err != nil || base != "http://prom.example.com:9090" {
		t.Errorf("normalized base URL = %q %v", base, err)
	}

	cfg := Config{BaseURL: "https://p.example.com", Auth: AuthConfig{Scheme: SchemeBearer, TenantID: "a\tb"}}
	if ce := cerrors.AsCLIError(ValidateService(cfg)); ce == nil || ce.Code != "BAD_TENANT" {
		t.Errorf("a tenant with a control character: %v", ce)
	}
	cfg.Auth = AuthConfig{Scheme: "token"}
	if ce := cerrors.AsCLIError(ValidateService(cfg)); ce == nil || ce.Code != "AUTH_BAD_SCHEME" || ce.Hint != "Use one of: none, basic, bearer." {
		t.Errorf("an unknown scheme: %+v", ce)
	}
}

// TestWording pins the command names in the context errors and hints.
func TestWording(t *testing.T) {
	cleanSetupEnv(t)
	dir := t.TempDir()
	_, err := Load(LoadOptions{ConfigDir: dir, DotenvPath: filepath.Join(dir, "absent.env"), Context: "ghost"})
	ce := cerrors.AsCLIError(err)
	if ce == nil || ce.Code != "UNKNOWN_CONTEXT" ||
		ce.Message != `context "ghost" (from --use-context) is not defined in the config file` ||
		ce.Hint != "Run `prometheus-cli config get-contexts` to list defined contexts." ||
		!reflect.DeepEqual(ce.NextSteps, []string{"prometheus-cli config get-contexts"}) {
		t.Errorf("UNKNOWN_CONTEXT = %+v", ce)
	}
	if hint := UnknownContextHint("ghost", []string{"alpha", "beta"}); hint != "Available contexts: alpha, beta." {
		t.Errorf("hint = %q", hint)
	}
}

// A `none` context has nothing to log in with, so its plan points at doctor
// instead of auth login, and offers no reuse. A context that authenticates
// without an identity is offered reuse before any sign-in step (the schemes are
// registered for this package's tests by reuse_test.go).
func TestPlanNextStepsFollowTheScheme(t *testing.T) {
	const reuse = "prometheus-cli --use-context 'team' auth reuse"
	const guide = "prometheus-cli --use-context 'team' auth guide"
	const login = "prometheus-cli --use-context 'team' auth login"
	for scheme, want := range map[string][]string{
		SchemeNone:   {guide, "prometheus-cli --use-context 'team' doctor"},
		SchemeBasic:  {reuse, guide, login},
		SchemeBearer: {reuse, guide, login},
	} {
		// The scheme comes from the flag, as a preset does; it does not limit
		// what reuse may adopt.
		resolved := &Resolved{Config: Config{BaseURL: "https://p.example.com", Auth: AuthConfig{Scheme: scheme}},
			Sources: map[string]string{fieldAuthScheme: "flag"}}
		plan, err := PlanServiceContext(File{}, "Team", resolved, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(plan.NextSteps, want) {
			t.Errorf("%s: next steps = %q", scheme, plan.NextSteps)
		}
	}
}
