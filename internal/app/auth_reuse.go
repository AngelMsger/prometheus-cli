// Reuse a verified native credential by associating it with an existing team
// context. agent-cli-kit/reuse decides; this file describes prometheus-cli's
// contexts to it and supplies resolution, verification and persistence.
// Secrets stay in their original store; public setup stays offline.
package app

import (
	"context"
	"reflect"
	"time"

	kitauth "github.com/angelmsger/agent-cli-kit/auth"
	"github.com/angelmsger/agent-cli-kit/credstore"
	"github.com/angelmsger/agent-cli-kit/reuse"
	"github.com/angelmsger/prometheus-cli/internal/auth"
	"github.com/angelmsger/prometheus-cli/internal/config"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/spf13/cobra"
)

type reuseServices struct {
	Resolve func(config.Config) (auth.Credential, credstore.Lookup, error)
	Lookup  func(key string) credstore.Lookup
	Verify  func(context.Context, config.Config, auth.Credential) error
	Read    func(string) (config.File, bool, error)
	Write   func(string, config.File) error
}

type reuseOptions struct {
	From       string
	FromScheme string
	DryRun     bool
}

func newAuthReuseCmd(s *appState) *cobra.Command {
	var opts reuseOptions
	cmd := &cobra.Command{
		Use:   "reuse",
		Short: "Reuse an existing login in the selected context without signing in again",
		Long: "Find a login this machine already verified for the same service (complete URL,\n" +
			"path included, and tenant) and point the selected context at it. Contexts\n" +
			"under the context's own scheme come first, then a password or bearer token\n" +
			"under the other scheme, including one stored for this service that no context\n" +
			"points at. A working login is never replaced automatically; a username recorded\n" +
			"under the bearer scheme is only a label. A `none` context needs no login and is\n" +
			"never a source. Never copy secrets, activate a context, or read credentials\n" +
			"from environment variables. Dry-run performs read-only verification and reports\n" +
			"the same proposed association. An unchanged or unavailable result is not proof\n" +
			"of authentication; use auth status to check.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := reuseAuthentication(s, opts, s.reuseServices())
			if err != nil {
				return err
			}
			return s.emit(result)
		},
	}
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "verify and preview the association without writing")
	cmd.Flags().StringVar(&opts.From, "from-context", "", "choose a matching source from config get-contexts when several logins are available")
	cmd.Flags().StringVar(&opts.FromScheme, "from-scheme", "", "use the credential stored for this service under this scheme (basic or bearer); with --from-context, paired with that context's username")
	return cmd
}

func (s *appState) reuseServices() reuseServices {
	return reuseServices{
		Resolve: func(cfg config.Config) (auth.Credential, credstore.Lookup, error) {
			return auth.ResolveLookup(cfg, config.Secrets{}, s.store)
		},
		Lookup: func(key string) credstore.Lookup { return s.store.Lookup(key) },
		Verify: func(ctx context.Context, cfg config.Config, cred auth.Credential) error {
			return verifyCredentialWithin(ctx, s, cfg, cred)
		},
		Read: config.ReadFile, Write: config.WriteFile,
	}
}

func reuseAuthentication(s *appState, opts reuseOptions, services reuseServices) (reuse.Result, error) {
	started := time.Now()
	file, _, err := services.Read(s.cfgDir)
	if err != nil {
		return reuse.Result{Context: s.resolved.ActiveContext, State: reuse.StateUnavailable, DryRun: opts.DryRun},
			cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ", "failed to read authentication contexts")
	}
	target, _ := file.Context(s.resolved.ActiveContext)
	targetCfg := config.StoredContext(target, file.Defaults)
	// Every source matched the destination's complete service and tenant, so a
	// credential is resolved and verified under the destination's configuration;
	// the tenant travels with it as X-Scope-OrgID.
	withCredential := func(spec reuse.Spec) config.Config {
		cfg := targetCfg
		cfg.BaseURL, cfg.Auth.Scheme, cfg.Auth.Username = spec.KeyBase, spec.Scheme, spec.Username
		return cfg
	}
	req := reuse.Request{
		Target:         s.resolved.ActiveContext,
		Contexts:       config.ReuseContexts(file),
		Runtime:        reuse.Context{Service: config.ReuseService(s.cfg()), Scheme: s.cfg().Auth.Scheme},
		SchemeSupplied: config.SchemeSupplied(s.resolved.Sources),
		From:           opts.From,
		FromScheme:     opts.FromScheme,
		DryRun:         opts.DryRun,
		Start:          started,
		Timeout:        s.timeout(),
		Check:          "auth status",
		Key:            auth.AccountKey,
		Valid: func(scheme string) bool {
			cfg := targetCfg
			cfg.Auth.Scheme = scheme
			return config.ValidateService(cfg) == nil
		},
		Resolve: func(spec reuse.Spec) (kitauth.Credential, credstore.Lookup, error) {
			cred, l, err := services.Resolve(withCredential(spec))
			return kitauth.Credential{Scheme: cred.Scheme, Username: cred.Username, Secret: cred.Secret}, l, err
		},
		Lookup: services.Lookup,
		Verify: func(ctx context.Context, spec reuse.Spec, cred kitauth.Credential) (reuse.Account, error) {
			cfg := withCredential(spec)
			return reuse.Account{}, services.Verify(ctx, cfg, auth.Credential{Scheme: cred.Scheme,
				Username: cred.Username, Secret: cred.Secret, TenantID: cfg.Auth.TenantID})
		},
		Persist: func(u reuse.Update) error {
			current, _, err := services.Read(s.cfgDir)
			if err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_READ", "failed to recheck authentication contexts")
			}
			if !reflect.DeepEqual(file, current) {
				return reuse.ErrConfigChanged
			}
			next := file
			next.Contexts = append([]config.NamedContext(nil), file.Contexts...)
			for i, c := range next.Contexts {
				if c.Name != target.Name {
					continue
				}
				c.Auth.Scheme = u.Scheme
				if u.SetUsername {
					c.Auth.Username = u.Username
				}
				if u.BaseURL != "" {
					c.BaseURL = u.BaseURL
				}
				next.Contexts[i] = c
			}
			return services.Write(s.cfgDir, next)
		},
	}
	return reuse.Run(req)
}

// withReuseRecovery leads a rejected-credential error (HTTP 401) with
// `auth reuse` when a stored login could replace the rejected one.
func withReuseRecovery(ce *cerrors.CLIError, s *appState, cmd *cobra.Command) {
	if ce.Category != cerrors.CategoryAuth || ce.HTTPStatus != 401 || s == nil || s.resolved == nil || cmd == nil {
		return
	}
	if cmd.Name() == "reuse" || cmd.Name() == "login" {
		return
	}
	if step := s.cfg().ReuseStep; step != "" {
		ce.NextSteps = append([]string{step}, ce.NextSteps...)
	}
}
