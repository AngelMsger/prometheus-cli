package config

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// serviceField excludes personal identity and secrets before environment
// inference. The tenant id is a service preset, not an identity: every member
// of a team queries the same multi-tenant backend under the same tenant.
func serviceField(field string) bool {
	switch field {
	case fieldFormat, fieldReadOnly, fieldServer, fieldAuthScheme, fieldCredentialURL, fieldTenantID:
		return true
	default:
		return false
	}
}

// resolveAuthDefaults is the hook the layered loader calls once the layers are
// merged. Prometheus needs no post-merge inference: the scheme already
// defaults to none, and env-based inference happens in layerFromVars.
func resolveAuthDefaults(values, sources map[string]string) {}

// NormalizeServiceURL retains the deployment path and rejects credential-bearing
// URLs. It is used at setup and persistence boundaries, never to derive an API
// route. A trailing /api/v1 is trimmed: pasting the endpoint instead of the
// server root is the most common Prometheus setup mistake, and silently storing
// it would turn every later command into a 404.
func NormalizeServiceURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL", "no service URL configured").
			WithNextSteps(constants.AppName + " config set-context <name> --base-url <url>")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(s, "\r\n\t") {
		return "", cerrors.New(cerrors.CategoryConfig, "BAD_BASE_URL",
			"service URL must be an HTTP(S) URL without credentials, query or fragment")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host := u.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), constants.APIPrefix)
	return strings.TrimRight(u.String(), "/"), nil
}

func validateCredentialURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") ||
		u.User != nil || strings.ContainsAny(raw, "\r\n\t") {
		return cerrors.New(cerrors.CategoryConfig, "BAD_CREDENTIAL_URL",
			"credential URL must be an absolute HTTP(S) page URL without embedded credentials")
	}
	return nil
}

// ValidateService checks only public setup fields and never probes a server.
func ValidateService(cfg Config) error {
	if _, err := NormalizeServiceURL(cfg.BaseURL); err != nil {
		return err
	}
	if err := validateCredentialURL(cfg.Auth.CredentialURL); err != nil {
		return err
	}
	switch cfg.Auth.Scheme {
	case SchemeNone, SchemeBasic, SchemeBearer:
	default:
		return cerrors.New(cerrors.CategoryConfig, "AUTH_BAD_SCHEME",
			"unsupported authentication scheme").
			WithHint("Use one of: " + strings.Join(Schemes(), ", ") + ".")
	}
	if strings.ContainsAny(cfg.Auth.TenantID, "\r\n\t") {
		return cerrors.New(cerrors.CategoryConfig, "BAD_TENANT",
			"tenant id must not contain control characters")
	}
	return nil
}

// FieldChange describes a public configuration change; credentials are never included.
type FieldChange struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// ContextPlan is shared by preview and execution. File is the exact proposed file.
type ContextPlan struct {
	Context        string                 `json:"context"`
	Changed        bool                   `json:"changed"`
	CurrentContext string                 `json:"current_context"`
	Changes        map[string]FieldChange `json:"changes"`
	NextSteps      []string               `json:"next_steps"`
	File           File                   `json:"-"`
}

// PlanServiceContext builds a non-secret, target-specific merge without side effects.
func PlanServiceContext(file File, name string, resolved *Resolved, overwrite, activate bool) (ContextPlan, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || strings.ContainsAny(name, "\r\n\t") {
		return ContextPlan{}, cerrors.New(cerrors.CategoryUsage, "CTX_NAME_EMPTY",
			"provide a non-empty context name")
	}
	old, exists := file.Context(name)
	if exists {
		name = old.Name
	}
	cfg := resolved.Config
	normalized, err := NormalizeServiceURL(cfg.BaseURL)
	if err != nil {
		return ContextPlan{}, err
	}
	cfg.BaseURL = normalized
	if err := ValidateService(cfg); err != nil {
		return ContextPlan{}, err
	}
	next := old
	next.Name = name
	changes := map[string]FieldChange{}
	conflicts := map[string]FieldChange{}
	merge := func(field string, dst *string, value string) {
		source := resolved.Sources[field]
		if exists && *dst != "" && source != "flag" && source != "env" && source != "dotenv" {
			return
		}
		before := *dst
		if field == fieldServer && before != "" {
			if normal, e := NormalizeServiceURL(before); e == nil && normal == value {
				return
			}
		}
		if before == value {
			return
		}
		change := FieldChange{Before: before, After: value}
		changes[field] = change
		if exists && before != "" {
			conflicts[field] = change
		}
		*dst = value
	}
	merge(fieldServer, &next.BaseURL, cfg.BaseURL)
	merge(fieldAuthScheme, &next.Auth.Scheme, cfg.Auth.Scheme)
	merge(fieldCredentialURL, &next.Auth.CredentialURL, cfg.Auth.CredentialURL)
	merge(fieldTenantID, &next.Auth.TenantID, cfg.Auth.TenantID)

	if len(conflicts) > 0 && !overwrite {
		return ContextPlan{}, cerrors.New(cerrors.CategoryConflict, "CONFIG_CONTEXT_CONFLICT",
			"team presets conflict with the existing context").
			WithDetails(conflicts).
			WithHint("Inspect the field differences; use --overwrite to update the supplied service fields, or choose another context name.").
			WithNextSteps(constants.AppName + " config set-context --help")
	}
	result := file
	result.Contexts = append([]NamedContext(nil), file.Contexts...)
	replaced := false
	for i, c := range result.Contexts {
		if c.Name == name {
			result.Contexts[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		result.Contexts = append(result.Contexts, next)
	}
	if len(file.Contexts) == 0 || activate {
		result.CurrentContext = name
	}
	if result.CurrentContext != file.CurrentContext {
		changes["current_context"] = FieldChange{Before: file.CurrentContext, After: result.CurrentContext}
	}
	quotedName := "'" + strings.ReplaceAll(name, "'", "'\"'\"'") + "'"
	steps := []string{constants.AppName + " --use-context " + quotedName + " auth guide"}
	// A `none` context needs no personal credential at all; pointing the member
	// at auth login would be a step that stores nothing.
	if next.Auth.Scheme != SchemeNone {
		steps = append(steps, constants.AppName+" --use-context "+quotedName+" auth login")
	} else {
		steps = append(steps, constants.AppName+" --use-context "+quotedName+" doctor")
	}
	return ContextPlan{
		Context:        name,
		Changed:        !reflect.DeepEqual(file, result),
		CurrentContext: result.CurrentContext,
		Changes:        changes,
		NextSteps:      steps,
		File:           result,
	}, nil
}

// AuthGuide is an offline acquisition guide, not a capability or authentication probe.
type AuthGuide struct {
	Server           string   `json:"server"`
	Flavor           string   `json:"flavor,omitempty"`
	Scheme           string   `json:"scheme"`
	CredentialURL    string   `json:"credential_url"`
	Source           string   `json:"source"`
	Instructions     []string `json:"instructions"`
	DocumentationURL string   `json:"documentation_url"`
	NextSteps        []string `json:"next_steps"`
}

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

// Lines returns the same guidance for plain prompts and terminal forms.
func (g AuthGuide) Lines() []string {
	lines := []string{
		fmt.Sprintf("Service: %s (%s)", g.Server, g.Scheme),
		"Credential page: " + g.CredentialURL,
	}
	return append(lines, g.Instructions...)
}

// WithCredentialGuide preserves host-store recovery and appends acquisition
// guidance only to absence errors, never to inaccessible-store errors.
func WithCredentialGuide(err error, cfg Config) error {
	ce := cerrors.AsCLIError(err)
	switch ce.Code {
	case "AUTH_NO_TOKEN", "AUTH_NO_BASIC", "CREDENTIAL_NOT_VISIBLE_OR_MISSING":
		if g, e := Guide(cfg, nil); e == nil {
			ce.NextSteps = append(ce.NextSteps, constants.AppName+" auth guide", "Credential page: "+g.CredentialURL)
		}
	}
	return ce
}
