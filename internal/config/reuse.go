package config

import (
	kit "github.com/angelmsger/agent-cli-kit/config"
	"github.com/angelmsger/agent-cli-kit/reuse"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
)

// ReuseContexts describes every stored context to agent-cli-kit/reuse with
// only its persisted fields and built-in defaults, so no environment identity
// leaks into a source.
func ReuseContexts(file File) []reuse.Context {
	out := make([]reuse.Context, 0, len(file.Contexts))
	for _, c := range file.Contexts {
		out = append(out, reuseContext(c, file.Defaults))
	}
	return out
}

func reuseContext(c NamedContext, defaults Defaults) reuse.Context {
	cfg := StoredContext(c, defaults)
	return reuse.Context{Name: c.Name, Service: ReuseService(cfg), KeyBase: c.BaseURL,
		Scheme: cfg.Auth.Scheme, Username: c.Auth.Username}
}

// ReuseService identifies a service for reuse: the complete normalized URL,
// path included, and the tenant, which every request sends as X-Scope-OrgID.
// An unusable URL identifies nothing.
func ReuseService(cfg Config) string {
	base, err := NormalizeServiceURL(cfg.BaseURL)
	if err != nil || base == "" {
		return ""
	}
	return base + "\x00" + cfg.Auth.TenantID
}

// SchemeSupplied reports whether the invocation supplied the scheme by flag,
// environment or .env, including one an environment credential implies.
func SchemeSupplied(sources map[string]string) bool {
	switch sources[fieldAuthScheme] {
	case "flag", "env", "dotenv":
		return true
	}
	return false
}

// reuseStep is the `auth reuse` step that goes before any sign-in step for the
// named context, or "" when an offline look at the file finds nothing reuse
// could adopt. A `none` context, a runtime username, or a service override that
// differs from the stored context means reuse would change nothing or be
// refused.
func reuseStep(cfg Config, sources map[string]string, file File, name string) string {
	stored, ok := file.Context(name)
	if !ok {
		return ""
	}
	target := reuseContext(stored, file.Defaults)
	if target.Service != ReuseService(cfg) || target.Scheme != cfg.Auth.Scheme {
		return ""
	}
	runtimeUser := ""
	switch sources[fieldAuthUsername] {
	case "flag", "env", "dotenv":
		runtimeUser = cfg.Auth.Username
	}
	if !reuse.MayReuse(target, ReuseContexts(file), runtimeUser, SchemeSupplied(sources), validUnder(StoredContext(stored, file.Defaults))) {
		return ""
	}
	step := constants.AppName + " --use-context " + kit.QuoteArg(stored.Name) + " auth reuse"
	if sources[fieldAuthScheme] == "flag" {
		step += " --auth-scheme " + cfg.Auth.Scheme
	}
	return step
}

// validUnder reports whether the context stays a valid configuration under a
// scheme, so reuse is not offered for a scheme its deployment rejects.
func validUnder(cfg Config) func(string) bool {
	return func(scheme string) bool {
		c := cfg
		c.Auth.Scheme = scheme
		return ValidateService(c) == nil
	}
}

// SchemeFlag is the scheme the invocation passed by flag, or "". Reuse repeats
// it in every command it suggests; a scheme from the environment or .env
// carries over by itself.
func SchemeFlag(cfg Config, sources map[string]string) string {
	if sources[fieldAuthScheme] == "flag" {
		return cfg.Auth.Scheme
	}
	return ""
}
