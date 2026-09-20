package errors

// defaultGuidance returns the default hint and next-step commands for a
// category. Callers may override these via WithHint / WithNextSteps when more
// specific guidance is available.
func defaultGuidance(cat Category) (hint string, steps []string) {
	switch cat {
	case CategoryUsage:
		return "The command was invoked incorrectly. Check flags and arguments.",
			[]string{"prometheus-cli <command> --help"}
	case CategoryConfig:
		return "No usable configuration was found or it is invalid.",
			[]string{"prometheus-cli config init", "prometheus-cli config show"}
	case CategoryAuth:
		return "The server rejected the credentials. The token or password may be wrong.",
			[]string{"prometheus-cli auth status", "prometheus-cli config init"}
	case CategoryPermission:
		return "The credentials are valid but lack permission for this Prometheus endpoint.",
			[]string{"prometheus-cli status buildinfo", "Verify the proxy or tenant policy in front of Prometheus allows this endpoint."}
	case CategoryNotFound:
		return "The requested Prometheus endpoint, metric or series does not exist.",
			[]string{"prometheus-cli metadata list", "prometheus-cli labels values __name__", "prometheus-cli series list --match '<selector>' --since 1h"}
	case CategoryConflict:
		return "The resource changed since it was last read (version conflict).",
			[]string{"Re-fetch the resource to get its current state, then retry."}
	case CategoryRateLimit:
		return "The server is rate limiting requests. Retry after a short wait.",
			[]string{"Wait and retry; narrow the time range, raise --step, or reduce --limit."}
	case CategoryNetwork:
		return "The server could not be reached (DNS, TLS or timeout).",
			[]string{"prometheus-cli doctor", "Check --base-url / PROMETHEUS_URL and network connectivity."}
	case CategoryServer:
		return "The Prometheus server returned an internal error.",
			[]string{"Retry later.", "prometheus-cli doctor"}
	case CategoryParse:
		return "A response could not be parsed or rendered.",
			[]string{"Retry with --format json and --verbose to inspect raw content."}
	default:
		return "An unexpected internal error occurred.",
			[]string{"Retry with --verbose for details."}
	}
}
