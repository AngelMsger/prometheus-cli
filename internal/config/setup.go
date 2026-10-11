package config

import (
	kit "github.com/angelmsger/agent-cli-kit/config"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
)

// NormalizeServiceURL retains the deployment path and rejects credential-bearing
// URLs; a bare host gets http://. It is used at setup and persistence
// boundaries, never to derive an API route. A trailing /api/v1 is trimmed:
// pasting the endpoint instead of the server root is the most common
// Prometheus setup mistake, and silently storing it would turn every later
// command into a 404.
func NormalizeServiceURL(raw string) (string, error) { return kit.NormalizeServiceURL(raw) }

// ValidateService checks only public setup fields and never probes a server.
func ValidateService(cfg Config) error { return kit.ValidateService(kit.Encode(cfg)) }

// FieldChange describes a public configuration change; credentials are never included.
type FieldChange = kit.FieldChange

// ContextPlan is shared by preview and execution. File is the exact proposed file.
type ContextPlan = kit.ContextPlan[NamedContext, Defaults]

// PlanServiceContext builds a non-secret, target-specific merge without side
// effects: the service URL, auth scheme, credential page and tenant from
// resolved.
func PlanServiceContext(file File, name string, resolved *Resolved, overwrite, activate bool) (ContextPlan, error) {
	plan, err := kit.PlanServiceContext(file, name, kit.Encode(resolved.Config), resolved.Sources, overwrite, activate)
	if err != nil {
		return plan, err
	}
	// A `none` context needs no personal credential at all; pointing the member
	// at auth login would be a step that stores nothing.
	if nc, _ := plan.File.Context(plan.Context); nc.Auth.Scheme == SchemeNone {
		plan.NextSteps[len(plan.NextSteps)-1] = constants.AppName + " --use-context " + kit.QuoteArg(plan.Context) + " doctor"
	}
	return plan, nil
}

// AuthGuide is an offline acquisition guide, not a capability or authentication probe.
type AuthGuide = kit.AuthGuide

// Guide derives display-only links. No request is ever sent to CredentialURL.
//
// Prometheus has no credential page of its own — it ships with no user
// database — so unlike the siblings there is no built-in URL to point at. The
// guide says that plainly and explains where the credential actually comes
// from for each scheme, rather than inventing a token page that does not exist.
func Guide(cfg Config, sources map[string]string) (AuthGuide, error) {
	if err := ValidateService(cfg); err != nil {
		return AuthGuide{}, err
	}
	base, _ := NormalizeServiceURL(cfg.BaseURL)
	g := AuthGuide{
		Server:           base,
		Scheme:           cfg.Auth.Scheme,
		CredentialURL:    base,
		Source:           "fallback",
		DocumentationURL: "https://prometheus.io/docs/prometheus/latest/configuration/https/",
		NextSteps:        []string{constants.AppName + " auth login"},
	}
	switch cfg.Auth.Scheme {
	case SchemeNone:
		g.Instructions = []string{
			"This context uses no authentication, which is how a Prometheus server on its own port is normally reached.",
			"No credential is needed. Run `" + constants.AppName + " doctor` to confirm the server answers.",
			"If the server is behind a gateway, switch the scheme with --auth-scheme basic or --auth-scheme bearer.",
		}
		g.NextSteps = []string{constants.AppName + " doctor"}
	case SchemeBasic:
		g.Instructions = []string{
			"Prometheus itself has no user accounts: HTTP Basic credentials are issued by whatever sits in front of it (a reverse proxy, an ingress, or its own web.yml basic_auth_users block).",
			"Ask the operator of that gateway for a username and password, or use the ones your team already distributes.",
			"Set --credential-url to your team's credential page so this guide points at it next time.",
		}
		g.DocumentationURL = "https://prometheus.io/docs/prometheus/latest/configuration/https/#http-server-config"
	case SchemeBearer:
		g.Instructions = []string{
			"A bearer token comes from the hosted vendor or platform in front of Prometheus (Grafana Cloud, Thanos, Cortex/Mimir, or a Kubernetes service account), not from Prometheus.",
			"Create the token in that product's own console and paste it at the prompt; it is stored in the OS keychain, never in the config file.",
			"For a multi-tenant backend also set --tenant <id>, which is sent as X-Scope-OrgID.",
		}
		g.DocumentationURL = "https://prometheus.io/docs/prometheus/latest/querying/api/"
	}
	if cfg.Auth.CredentialURL != "" {
		g.CredentialURL = cfg.Auth.CredentialURL
		g.Source = sources[fieldCredentialURL]
		if g.Source == "" {
			g.Source = "config"
		}
	}
	return g, nil
}

// WithCredentialGuide preserves host-store recovery and appends acquisition
// guidance only to absence errors, never to inaccessible-store errors.
func WithCredentialGuide(err error, cfg Config) error {
	return kit.WithCredentialGuide(err, func() (AuthGuide, error) { return Guide(cfg, nil) })
}
