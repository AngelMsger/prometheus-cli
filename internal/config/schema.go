package config

import (
	"path/filepath"
	"strconv"
	"strings"

	kit "github.com/angelmsger/agent-cli-kit/config"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
)

// EnvNoContextHint opts out of the runtime multi-context notice.
const EnvNoContextHint = "PROMETHEUS_CLI_NO_CONTEXT_HINT"

func init() {
	kit.Register(&kit.Schema{
		// Each row names a field's environment variables and built-in
		// default. Service fields are the team presets a setup load keeps;
		// the user name and secrets are personal. The tenant id is a service
		// preset, not an identity: every member of a team queries the same
		// multi-tenant backend under the same tenant.
		Fields: []kit.Field{
			{Key: fieldServer, Env: []string{"PROMETHEUS_URL"}, Service: true},
			{Key: fieldCredentialURL, Env: []string{"PROMETHEUS_CREDENTIAL_URL"}, Service: true},
			{Key: fieldAuthScheme, Env: []string{"PROMETHEUS_AUTH_SCHEME"}, Default: SchemeNone, Service: true},
			{Key: fieldAuthUsername, Env: []string{"PROMETHEUS_USER"}},
			{Key: fieldTenantID, Env: []string{"PROMETHEUS_TENANT"}, Service: true},
			{Key: fieldFormat, Env: []string{"PROMETHEUS_FORMAT"}, Default: constants.DefaultFormat, Service: true},
			{Key: fieldTimeout, Default: constants.DefaultTimeout.String()},
			{Key: fieldMaxRetries, Default: strconv.Itoa(constants.DefaultMaxRetries)},
			{Key: fieldReadOnly, Env: []string{"PROMETHEUS_CLI_READ_ONLY"}, Service: true},
			// A secret in the environment selects its scheme; a bearer token
			// wins over a password.
			{Key: fieldToken, Env: []string{"PROMETHEUS_TOKEN"}, Implies: SchemeBearer},
			{Key: fieldPassword, Env: []string{"PROMETHEUS_PASSWORD"}, Implies: SchemeBasic},
		},
		Paths: kit.Paths{
			Dir:  filepath.Join(constants.ConfigParentDirName, constants.ConfigDirName),
			File: constants.ConfigFileName,
		},
		Words: kit.Words{
			App:          constants.AppName,
			ContextFlag:  "--use-context",
			ContextEnv:   "PROMETHEUS_CONTEXT",
			NoHintEnv:    EnvNoContextHint,
			ListContexts: "config contexts",
			UseContext:   "config use-context",
			SetContext:   "config set-context",
			AuthGuide:    "auth guide",
			AuthLogin:    "auth login",
		},
		Schemes: Schemes(),
		URL: kit.URLRules{
			// A bare host means a local http:// install, as `config init`
			// assumes.
			AssumeScheme: "http",
			// Pasting the API endpoint instead of the server root is the most
			// common Prometheus setup mistake, and storing it would turn every
			// later command into a 404.
			TrimSuffix: constants.APIPrefix,
		},
		Validate: validateTenant,
	})
}

// validateTenant rejects a tenant id that could not travel as the
// X-Scope-OrgID header.
func validateTenant(v kit.Values) error {
	if strings.ContainsAny(v[fieldTenantID], "\r\n\t") {
		return cerrors.New(cerrors.CategoryConfig, "BAD_TENANT",
			"tenant id must not contain control characters")
	}
	return nil
}
