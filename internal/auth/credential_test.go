package auth

import (
	"net/http"
	"testing"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// `none` is a complete posture, not an incomplete one: a plain Prometheus
// needs no credential, so nothing must demand one.
func TestNoneSchemeNeedsNoSecret(t *testing.T) {
	t.Parallel()
	cred := Credential{Scheme: SchemeNone}
	if cred.NeedsSecret() {
		t.Fatal("the none scheme should not require a secret")
	}
	if err := cred.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if got := cred.Header(); got != "" {
		t.Fatalf("Header() = %q, want empty", got)
	}
}

func TestValidateRejectsIncompleteCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		cred Credential
		code string
		msg  string
	}{
		{"basic without a password", Credential{Scheme: SchemeBasic, Username: "u"}, "AUTH_NO_BASIC", "basic auth requires both a username and a password"},
		{"basic without a username", Credential{Scheme: SchemeBasic, Secret: "s"}, "AUTH_NO_BASIC", "basic auth requires both a username and a password"},
		{"bearer without a token", Credential{Scheme: SchemeBearer}, "AUTH_NO_TOKEN", "bearer auth requires a token"},
		{"an unknown scheme", Credential{Scheme: "oauth"}, "AUTH_BAD_SCHEME", `unknown auth scheme "oauth" (want none, basic or bearer)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ce := cerrors.AsCLIError(tc.cred.Validate())
			if ce == nil || ce.Code != tc.code || ce.Message != tc.msg {
				t.Fatalf("got %+v, want %s %q", ce, tc.code, tc.msg)
			}
		})
	}
}

// A multi-tenant backend answers an untenanted request with an empty result
// rather than an error, so the header has to travel on every request —
// including the ones that carry no Authorization at all.
func TestDecoratorAppliesTheTenantHeaderEvenWithoutAuth(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.test", nil)
	Credential{Scheme: SchemeNone, TenantID: "team-a"}.Decorator()(req)
	if got := req.Header.Get("X-Scope-OrgID"); got != "team-a" {
		t.Fatalf("tenant header = %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("none scheme set an Authorization header: %q", got)
	}
}

func TestDecoratorAppliesBothHeaders(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.test", nil)
	Credential{Scheme: SchemeBearer, Secret: "tok", TenantID: "team-a"}.Decorator()(req)
	if req.Header.Get("Authorization") != "Bearer tok" || req.Header.Get("X-Scope-OrgID") != "team-a" {
		t.Fatalf("headers = %v", req.Header)
	}
}

// One host can front several deployments under different path prefixes, so the
// keychain key has to include the path or the two would share one secret.
func TestAccountKeyDistinguishesPathRoutedDeployments(t *testing.T) {
	t.Parallel()
	if got := AccountKey("https://gw.example.com/team-a/", SchemeBearer); got != "gw.example.com/team-a:bearer" {
		t.Fatalf("path-routed AccountKey = %q", got)
	}
	if got := AccountKey("http://localhost:9090", SchemeNone); got != "localhost:9090:none" {
		t.Fatalf("AccountKey = %q", got)
	}
}

// The tenant is not a secret, so a redacted credential still names it.
func TestRedactedKeepsTheTenant(t *testing.T) {
	t.Parallel()
	got := Credential{Scheme: SchemeBearer, Secret: "supersecrettoken", TenantID: "team-a"}.Redacted()
	if got.Secret == "supersecrettoken" || got.TenantID != "team-a" || got.Scheme != SchemeBearer {
		t.Fatalf("Redacted() = %+v", got)
	}
}
