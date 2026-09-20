package app

import (
	"github.com/angelmsger/prometheus-cli/internal/auth"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/spf13/cobra"
)

func newAuthCmd(s *appState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Log in, check reachability and log out",
		Long: "Prometheus has no accounts of its own, so a context often needs no\n" +
			"credential at all (the `none` scheme). `auth login` stores the secret for\n" +
			"the basic / bearer schemes used by a proxy or hosted endpoint; `auth\n" +
			"status` reports what this context will send and whether the server answers.",
	}
	cmd.AddCommand(newAuthGuideCmd(s), newAuthLoginCmd(s), newAuthStatusCmd(s), newAuthLogoutCmd(s))
	return cmd
}

func newAuthLoginCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Store credentials for the active context (interactive)",
		Long: "Prompts for the credential this context's auth scheme needs — a username\n" +
			"and password (basic) or a bearer token (bearer) — verifies it against the\n" +
			"server, and stores the secret in the OS keychain. The `none` scheme has\n" +
			"nothing to store. Requires an interactive terminal; in CI / agent sandboxes\n" +
			"set PROMETHEUS_TOKEN (or PROMETHEUS_USER + PROMETHEUS_PASSWORD) instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			if cfg.BaseURL == "" {
				return cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL",
					"no server configured yet").
					WithNextSteps("prometheus-cli config init")
			}
			scheme := cfg.Auth.Scheme
			if scheme == "" {
				scheme = auth.SchemeNone
			}
			if scheme == auth.SchemeNone {
				return cerrors.New(cerrors.CategoryUsage, "AUTH_NOT_REQUIRED",
					"this context uses the `none` auth scheme, which stores no credential").
					WithHint("Prometheus itself has no accounts. Switch the scheme only if a proxy "+
						"or hosted endpoint sits in front of it.").
					WithNextSteps(
						"prometheus-cli doctor",
						"prometheus-cli config set-context <name> --auth-scheme bearer",
						"prometheus-cli config set-context <name> --auth-scheme basic")
			}
			if !stdinIsTTY() {
				return cerrors.New(cerrors.CategoryAuth, "AUTH_LOGIN_NEEDS_TTY",
					"auth login requires an interactive terminal").
					WithHint("Set PROMETHEUS_TOKEN (or PROMETHEUS_USER + PROMETHEUS_PASSWORD), "+
						"or run `prometheus-cli config init` in a terminal.").
					WithNextSteps("prometheus-cli auth status", "prometheus-cli config init")
			}
			cred := auth.Credential{Scheme: scheme, Username: cfg.Auth.Username, TenantID: cfg.Auth.TenantID}
			if _, _, err := loginFile(s, cfg, cred); err != nil {
				return err
			}
			if err := printAuthGuide(cfg); err != nil {
				return err
			}
			if scheme == auth.SchemeBasic && cred.Username == "" {
				u, err := promptLine("Username", "")
				if err != nil {
					return err
				}
				cred.Username = u
			}
			secretLabel := "Bearer token"
			if scheme == auth.SchemeBasic {
				secretLabel = "Password"
			}
			secret, err := promptSecret(secretLabel)
			if err != nil {
				return err
			}
			cred.Secret = secret

			backend, err := completeLogin(s, cfg, cred, s.loginServices())
			if err != nil {
				return err
			}
			return s.emit(map[string]any{
				"logged_in": true,
				"base_url":  cfg.BaseURL,
				"username":  cred.Username,
				"scheme":    scheme,
				"stored_in": backend,
			})
		},
	}
}

func newAuthStatusCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what this context sends and whether the server answers",
		Long: "Prometheus exposes no identity endpoint, so there is no \"who am I\" to\n" +
			"report. This shows the resolved server, scheme, username and tenant, then\n" +
			"calls the build-info endpoint to prove the server is reachable and accepts\n" +
			"what the context sends.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			out := map[string]any{
				"base_url": cfg.BaseURL,
				"scheme":   cfg.Auth.Scheme,
				"username": cfg.Auth.Username,
				"tenant":   cfg.Auth.TenantID,
				"context":  s.resolved.ActiveContext,
			}
			ctx, cancel := cmdContext(s)
			defer cancel()
			client, err := s.newClient()
			if err != nil {
				out["authenticated"] = false
				out["error"] = err.Error()
				return s.emit(out)
			}
			info, err := client.BuildInfo(ctx)
			if err != nil {
				out["authenticated"] = false
				out["error"] = err.Error()
				return s.emit(out)
			}
			out["authenticated"] = true
			if info.Version != "" {
				out["server_version"] = info.Version
			}
			if info.Revision != "" {
				out["server_revision"] = info.Revision
			}
			return s.emit(out)
		},
	}
}

func newAuthLogoutCmd(s *appState) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored credential for the active context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := s.cfg()
			if cfg.BaseURL == "" {
				return cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL",
					"no server configured").WithNextSteps("prometheus-cli config init")
			}
			scheme := cfg.Auth.Scheme
			if scheme == "" {
				scheme = auth.SchemeNone
			}
			if err := auth.Forget(cfg.BaseURL, scheme, s.store); err != nil {
				return cerrors.Wrap(err, cerrors.CategoryConfig, "LOGOUT_FAILED",
					"failed to remove stored credential")
			}
			return s.emit(map[string]any{"logged_out": true, "base_url": cfg.BaseURL, "scheme": scheme})
		},
	}
}

// verifyAndSave builds a client from cred, calls the server to confirm the
// credential works, then persists the secret. It returns the storage backend.
func verifyAndSave(s *appState, baseURL string, cred auth.Credential) (string, error) {
	cfg := s.cfg()
	cfg.BaseURL = baseURL

	if err := verifyCredential(s, cfg, cred); err != nil {
		return "", err
	}
	return auth.Save(baseURL, cred, s.store)
}
