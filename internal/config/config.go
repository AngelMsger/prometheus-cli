// Package config resolves CLI configuration from layered sources: CLI flags,
// environment variables, a .env file, a YAML config file and built-in
// defaults, in that precedence order (highest first).
//
// The engine — layering, provenance, context selection, the atomic file
// write and team presets — comes from agent-cli-kit/config. This package
// registers prometheus-cli's fields, environment variables, file location,
// command wording and tenant check with it (schema.go), keeps the YAML shape
// of the file (file.go), and keeps the names the rest of the repo uses.
//
// Secrets (bearer tokens, passwords) are never stored in the YAML config file.
// They are surfaced through Resolved.Secrets when supplied via flags/env/.env,
// or loaded from the OS keychain by the auth package.
package config

import (
	"time"

	kit "github.com/angelmsger/agent-cli-kit/config"
)

// Auth scheme values.
//
// Prometheus itself ships without authentication, so `none` is a first-class
// scheme and the default — unlike the sibling CLIs, whose products always
// authenticate. Hosted and proxied deployments (a reverse proxy, Grafana Cloud,
// Thanos, Cortex/Mimir) put HTTP Basic or a bearer token in front of it, which
// the other two schemes cover.
const (
	// SchemeNone sends no Authorization header. It is the default, and the
	// right choice for a Prometheus reachable on its own port.
	SchemeNone = "none"
	// SchemeBasic is username+password HTTP Basic auth, the usual reverse-proxy
	// arrangement.
	SchemeBasic = "basic"
	// SchemeBearer sends `Authorization: Bearer <token>`, used by hosted
	// endpoints and by Kubernetes service-account access.
	SchemeBearer = "bearer"
)

// Schemes lists the supported auth schemes in the order they are offered.
func Schemes() []string { return []string{SchemeNone, SchemeBasic, SchemeBearer} }

// DefaultContextName is the name given to an unnamed context.
const DefaultContextName = kit.DefaultContextName

// Context selection sources, reported on Resolved.ContextSource. The first two
// are explicit (the caller named a context for this invocation); the rest are
// implicit (the CLI fell back to a stored or sole context).
const (
	ContextSourceFlag    = kit.ContextSourceFlag    // --use-context
	ContextSourceEnv     = kit.ContextSourceEnv     // PROMETHEUS_CONTEXT
	ContextSourceCurrent = kit.ContextSourceCurrent // the file's current_context
	ContextSourceSingle  = kit.ContextSourceSingle  // the sole defined context
	ContextSourceDefault = kit.ContextSourceDefault // a context literally named "default"
	ContextSourceNone    = kit.ContextSourceNone    // nothing selected (no/empty config)
)

// NamedContext is one named Prometheus server profile inside the config file.
// Runtime defaults are shared across contexts and live in File.Defaults.
type NamedContext struct {
	Name    string `config:"name"`
	BaseURL string `config:"server"`
	Auth    AuthConfig
}

// Config holds the resolved, non-secret configuration.
type Config struct {
	// CredentialBaseURL preserves an equivalent stored URL's native lookup key.
	// It is runtime-only and never changes the request destination.
	CredentialBaseURL string     `yaml:"-" json:"-"`
	BaseURL           string     `yaml:"server" config:"server"`
	Auth              AuthConfig `yaml:"auth"`
	Defaults          Defaults   `yaml:"defaults"`
}

// AuthConfig holds non-secret auth settings.
type AuthConfig struct {
	CredentialURL string `yaml:"credential_url,omitempty" config:"auth.credential_url"`
	Scheme        string `yaml:"scheme" config:"auth.scheme"`
	Username      string `yaml:"username,omitempty" config:"auth.username"`
	// TenantID is sent as the X-Scope-OrgID header. Multi-tenant
	// Prometheus-compatible backends (Cortex, Mimir, Thanos Receive) require it
	// on every request and answer with an empty result set — not an error —
	// when it is missing, which is a silent wrong answer for an agent.
	TenantID string `yaml:"tenant_id,omitempty" config:"auth.tenant_id"`
}

// Defaults holds tunable runtime defaults.
type Defaults struct {
	Format     string        `yaml:"format" config:"defaults.format"`
	Timeout    time.Duration `yaml:"timeout" config:"defaults.timeout"`
	MaxRetries int           `yaml:"max_retries" config:"defaults.max_retries"`
	// ReadOnly blocks every mutating client method. Settable from the config
	// file, from PROMETHEUS_CLI_READ_ONLY, or temporarily overridden via
	// --allow-writes.
	ReadOnly bool `yaml:"read_only,omitempty" config:"defaults.read_only"`
}

// Secrets holds credentials observed in non-file layers. Empty fields mean the
// secret was not supplied via flags/env/.env and must come from the keychain.
type Secrets struct {
	Password string `config:"secret.password"`
	Token    string `config:"secret.token"`
}

// Resolved is the outcome of Load: the merged Config plus provenance and any
// transient secrets.
type Resolved struct {
	Config  Config
	Secrets Secrets
	// Sources maps a field key to the layer name that supplied its final
	// value: "flag", "env", "dotenv", "file", "default".
	Sources map[string]string
	// ActiveContext is the name of the context whose fields were applied.
	// Empty when no config file (or no contexts) exists — pure-env usage.
	ActiveContext string
	// ContextSource records which precedence rule chose ActiveContext (one of
	// the ContextSource* constants), so callers can tell an explicit choice
	// (flag/env) from an implicit fallback (current_context, sole, default).
	ContextSource string
	// ContextNames lists every context defined in the file, in file order.
	ContextNames []string
}

// ContextSelectedExplicitly reports whether the active context was chosen for
// this invocation (via --use-context or PROMETHEUS_CONTEXT) rather than fallen
// back to from the file's current_context, the sole context, or a "default"
// context. It is the signal used to decide whether to nudge an agent that may
// not realise which of several servers it is hitting.
func (r *Resolved) ContextSelectedExplicitly() bool {
	return r.ContextSource == ContextSourceFlag || r.ContextSource == ContextSourceEnv
}

// Field keys used for layer maps and provenance tracking.
const (
	fieldCredentialURL = kit.KeyCredentialURL
	fieldServer        = kit.KeyServer
	fieldAuthScheme    = kit.KeyAuthScheme
	fieldAuthUsername  = "auth.username"
	fieldTenantID      = "auth.tenant_id"
	fieldFormat        = "defaults.format"
	fieldTimeout       = "defaults.timeout"
	fieldMaxRetries    = "defaults.max_retries"
	fieldReadOnly      = "defaults.read_only"
	// Secret field keys (never persisted to the YAML file).
	fieldPassword = "secret.password"
	fieldToken    = "secret.token"
)

// Field key accessors for callers outside this package (e.g. config show).
const (
	FieldServer        = fieldServer
	FieldCredentialURL = fieldCredentialURL
	FieldAuthScheme    = fieldAuthScheme
	FieldAuthUser      = fieldAuthUsername
	FieldTenantID      = fieldTenantID
	FieldFormat        = fieldFormat
	FieldTimeout       = fieldTimeout
	FieldMaxRetries    = fieldMaxRetries
	FieldReadOnly      = fieldReadOnly
)
