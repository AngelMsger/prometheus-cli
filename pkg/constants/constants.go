// Package constants holds project-wide constants and build-time metadata.
package constants

import "time"

// Build-time metadata, injected via -ldflags. See Makefile.
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

const (
	// AppName is the binary / command name.
	AppName = "prometheus-cli"

	// EnvPrefix is the environment variable prefix for all settings.
	EnvPrefix = "PROMETHEUS_"

	// ConfigParentDirName groups every angelmsger CLI's per-user config under
	// one shared $HOME-relative directory (~/.angelmsger).
	ConfigParentDirName = ".angelmsger"

	// ConfigDirName is the per-CLI config directory under ConfigParentDirName,
	// i.e. ~/.angelmsger/prometheus.
	ConfigDirName = "prometheus"

	// ConfigFileName is the YAML config file within ConfigDirName.
	ConfigFileName = "config.yaml"

	// CredentialsFileName is the fallback secret store when no keychain is available.
	CredentialsFileName = "credentials"

	// KeychainService is the service name used for OS keychain entries.
	KeychainService = "prometheus-cli"
)

// Defaults for runtime behaviour.
const (
	DefaultFormat     = "json"
	DefaultTimeout    = 30 * time.Second
	DefaultMaxRetries = 3

	// DefaultLocalBaseURL is the default URL of a local Prometheus server.
	DefaultLocalBaseURL = "http://localhost:9090"

	// APIPrefix is the versioned HTTP API root every endpoint hangs off.
	APIPrefix = "/api/v1"

	// MaxRangePoints is Prometheus' own hard limit on the number of points a
	// range query may resolve to ("exceeded maximum resolution of 11,000
	// points"). The CLI derives and clamps --step against it so an agent never
	// has to discover the limit by hitting it.
	MaxRangePoints = 11000

	// TargetRangePoints is the number of points an auto-derived --step aims
	// for: dense enough to show shape, small enough to stay inside an agent's
	// context window.
	TargetRangePoints = 250

	// DefaultSeriesLimit bounds `series list`, `labels list` and
	// `labels values` when the caller gives no --limit, so a stray discovery
	// call on a large instance cannot flood an agent's context. 0 disables it.
	DefaultSeriesLimit = 1000

	// DefaultMetadataLimit bounds `metadata list` for the same reason.
	DefaultMetadataLimit = 500
)

// UserAgent identifies the CLI to the Prometheus server.
func UserAgent() string {
	return AppName + "/" + Version
}
