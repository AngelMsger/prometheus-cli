// Package auth models Prometheus credentials, resolves them from configuration
// or secure storage, and applies them to outgoing HTTP requests.
package auth

import (
	"net/url"
	"strings"

	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
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

// Credential is a fully resolved credential ready to authenticate requests.
type Credential struct {
	Scheme   string
	Username string // the login the secret belongs to (basic only)
	Secret   string // password (basic) or bearer token (bearer)
	// TenantID is sent as X-Scope-OrgID for multi-tenant backends. It is not a
	// secret, and it is carried here because it identifies the caller on every
	// request exactly like the credential does.
	TenantID string
}

// NeedsSecret reports whether the scheme requires a stored secret at all.
// A `none` credential is complete as soon as it exists, so nothing prompts for
// a secret, nothing is written to the keychain, and nothing fails when the
// keychain is unreachable.
func (c Credential) NeedsSecret() bool { return c.Scheme != SchemeNone }

// Validate reports whether the credential is internally consistent.
func (c Credential) Validate() error {
	switch c.Scheme {
	case SchemeNone:
		return nil
	case SchemeBasic:
		if c.Username == "" || c.Secret == "" {
			return cerrors.New(cerrors.CategoryConfig, "AUTH_NO_BASIC",
				"basic auth requires both a username and a password")
		}
	case SchemeBearer:
		if c.Secret == "" {
			return cerrors.New(cerrors.CategoryConfig, "AUTH_NO_TOKEN",
				"bearer auth requires a token")
		}
	default:
		return cerrors.Newf(cerrors.CategoryConfig, "AUTH_BAD_SCHEME",
			"unknown auth scheme %q (want none, basic or bearer)", c.Scheme)
	}
	return nil
}

// Redacted returns a copy safe for logging: the secret is masked.
func (c Credential) Redacted() Credential {
	c.Secret = maskSecret(c.Secret)
	return c
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

// AccountKey derives the keychain account identifier for a base URL and scheme.
// It is stable across runs so credentials can be located later.
func AccountKey(baseURL, scheme string) string {
	host := baseURL
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Host
		if p := strings.Trim(u.Path, "/"); p != "" {
			// A path-routed deployment (one host serving several tenants under
			// different prefixes) needs its own entry, not the host's.
			host += "/" + p
		}
	}
	return host + ":" + scheme
}
