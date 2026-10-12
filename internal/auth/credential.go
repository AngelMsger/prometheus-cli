// Package auth declares prometheus-cli's auth schemes and resolves credentials
// from configuration or secure storage. Credential validation, header encoding
// and the recovery errors come from agent-cli-kit/auth; this package registers
// Prometheus's schemes and wording with it and adds the tenant header.
package auth

import (
	"net/http"

	kit "github.com/angelmsger/agent-cli-kit/auth"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	_ "github.com/angelmsger/prometheus-cli/pkg/errors" // installs Prometheus's error guidance
	"github.com/angelmsger/prometheus-cli/pkg/transport"
)

// Scheme identifies an authentication scheme.
//
// Prometheus has no authentication of its own, so SchemeNone is both supported
// and the default; SchemeBasic and SchemeBearer cover the gateway, hosted and
// multi-tenant deployments that do authenticate.
const (
	// SchemeNone sends no Authorization header.
	SchemeNone = "none"
	// SchemeBasic is a username plus a password, encoded as HTTP Basic.
	SchemeBasic = "basic"
	// SchemeBearer sends the secret as an RFC 6750 bearer token.
	SchemeBearer = "bearer"
)

func init() {
	kit.Register(&kit.Product{
		Schemes: []kit.Scheme{
			{Name: SchemeNone, Kind: kit.None},
			{Name: SchemeBasic, Kind: kit.Basic,
				Missing: "basic auth requires both a username and a password"},
			{Name: SchemeBearer, Kind: kit.Bearer,
				Missing: "bearer auth requires a token"},
		},
		DefaultScheme:    SchemeNone,
		Name:             "Prometheus",
		Doctor:           constants.AppName + " doctor",
		MissingStep:      "Only if the host retry also reports missing credentials, run `" + constants.AppName + " config init` in the user's terminal or set PROMETHEUS_* environment variables.",
		InaccessibleStep: "Do not run `config init` unless the same check also fails in the host environment.",
		ListSchemes:      true,
		// prometheus-cli has always keyed secrets by host and URL path, so one
		// gateway host fronting several path-routed deployments keeps one
		// keychain entry per deployment.
		AccountKeyIncludesPath: true,
	})
}

// tenantHeader is the multi-tenancy header Cortex, Mimir and Thanos Receive
// read. Sending it to a single-tenant Prometheus is harmless: unknown headers
// are ignored.
const tenantHeader = "X-Scope-OrgID"

// Credential is a fully resolved credential ready to authenticate requests.
// Its Validate, Header, Redacted and NeedsSecret methods follow the schemes
// registered above; Decorator also sends the tenant.
type Credential struct {
	Scheme   string
	Username string // the login the secret belongs to (basic only)
	Secret   string // password (basic) or bearer token (bearer)
	// TenantID is sent as X-Scope-OrgID for multi-tenant backends. It is not a
	// secret, and it is carried here because it identifies the caller on every
	// request exactly like the credential does.
	TenantID string
}

// kitCredential is c without the tenant, as agent-cli-kit/auth models it.
func (c Credential) kitCredential() kit.Credential {
	return kit.Credential{Scheme: c.Scheme, Username: c.Username, Secret: c.Secret}
}

// NeedsSecret reports whether the scheme requires a stored secret at all.
// A `none` credential is complete as soon as it exists, so nothing prompts for
// a secret, nothing is written to the keychain, and nothing fails when the
// keychain is unreachable.
func (c Credential) NeedsSecret() bool { return c.kitCredential().NeedsSecret() }

// Validate reports whether the credential is internally consistent.
func (c Credential) Validate() error { return c.kitCredential().Validate() }

// Header returns the Authorization header value for the credential, or "" for
// the `none` scheme.
func (c Credential) Header() string { return c.kitCredential().Header() }

// Decorator returns a transport.Decorator that authenticates every request and
// applies the tenant header. It is never nil, so a `none` credential still
// carries the tenant id.
func (c Credential) Decorator() transport.Decorator {
	authenticate := c.kitCredential().Decorator()
	tenant := c.TenantID
	return func(req *http.Request) {
		authenticate(req)
		if tenant != "" {
			req.Header.Set(tenantHeader, tenant)
		}
	}
}

// Redacted returns a copy safe for logging: the secret is masked and the
// tenant, which is not a secret, is kept.
func (c Credential) Redacted() Credential {
	c.Secret = c.kitCredential().Redacted().Secret
	return c
}

// AccountKey derives the keychain account identifier for a base URL and scheme.
// It is stable across runs so credentials can be located later, and it
// includes the URL path (see AccountKeyIncludesPath above).
func AccountKey(baseURL, scheme string) string { return kit.AccountKey(baseURL, scheme) }
