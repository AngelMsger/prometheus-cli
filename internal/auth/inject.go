package auth

import (
	"encoding/base64"
	"net/http"

	"github.com/angelmsger/prometheus-cli/pkg/transport"
)

// tenantHeader is the multi-tenancy header Cortex, Mimir and Thanos Receive
// read. Sending it to a single-tenant Prometheus is harmless: unknown headers
// are ignored.
const tenantHeader = "X-Scope-OrgID"

// Header returns the Authorization header value for the credential, or "" for
// the `none` scheme.
func (c Credential) Header() string {
	switch c.Scheme {
	case SchemeBasic:
		raw := c.Username + ":" + c.Secret
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
	case SchemeBearer:
		return "Bearer " + c.Secret
	default:
		return ""
	}
}

// Decorator returns a transport.Decorator that authenticates every request and
// applies the tenant header. It is never nil, so a `none` credential still
// carries the tenant id.
func (c Credential) Decorator() transport.Decorator {
	header := c.Header()
	tenant := c.TenantID
	return func(req *http.Request) {
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		if tenant != "" {
			req.Header.Set(tenantHeader, tenant)
		}
	}
}
