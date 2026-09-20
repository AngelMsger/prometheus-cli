package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/angelmsger/prometheus-cli/internal/auth"
	"github.com/angelmsger/prometheus-cli/internal/config"
	"github.com/angelmsger/prometheus-cli/internal/output"
	"github.com/angelmsger/prometheus-cli/pkg/apiclient"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/transport"
)

// globalFlags holds the persistent flags shared by every command.
type globalFlags struct {
	authScheme    string
	credentialURL string
	setupContext  string
	baseURL       string
	tenant        string
	format        string
	fields        string
	timeout       string
	configPath    string
	useContext    string
	verbose       bool
	// pretty opts a human user into TUI prompts (in `config init`) and
	// ANSI-colored JSON. Off by default so agent / scripted / pipe usage stays
	// byte-identical.
	pretty bool
	// allowWrites overrides read-only mode for the current invocation.
	allowWrites bool
}

// appState is the shared runtime context, built once in the root command's
// PersistentPreRunE and captured by every subcommand handler.
type appState struct {
	gflags   globalFlags
	resolved *config.Resolved
	store    *auth.Store
	cfgDir   string
}

// load resolves configuration from all sources using the current global flags.
func (s *appState) load() error {
	cfgDir := s.gflags.configPath
	if cfgDir == "" {
		d, err := config.ResolveConfigDir()
		if err != nil {
			return cerrors.Wrap(err, cerrors.CategoryConfig, "NO_HOME",
				"could not determine the home directory")
		}
		cfgDir = d
	}
	resolved, err := config.Load(config.LoadOptions{
		ConfigDir: cfgDir,
		Context:   s.loadContext(),
		Setup:     s.gflags.setupContext != "",
		Flags: config.FlagValues{
			BaseURL:       s.gflags.baseURL,
			AuthScheme:    s.gflags.authScheme,
			CredentialURL: s.gflags.credentialURL,
			TenantID:      s.gflags.tenant,
			Format:        s.gflags.format,
			Timeout:       s.gflags.timeout,
		},
	})
	if err != nil {
		// Pass structured CLI errors (e.g. UNKNOWN_CONTEXT) through untouched.
		var ce *cerrors.CLIError
		if errors.As(err, &ce) {
			return ce
		}
		return cerrors.Wrap(err, cerrors.CategoryConfig, "CONFIG_LOAD",
			"failed to load configuration")
	}
	s.resolved = resolved
	s.cfgDir = cfgDir
	s.store = auth.NewStore(cfgDir)
	return nil
}

// cfg returns the resolved config.
func (s *appState) cfg() config.Config { return s.resolved.Config }

// newClient resolves credentials and builds an authenticated API client.
func (s *appState) newClient() (apiclient.Client, error) {
	cfg := s.cfg()
	cred, err := auth.Resolve(cfg, s.resolved.Secrets, s.store)
	if err != nil {
		return nil, err
	}
	var extra []transport.Decorator
	if s.gflags.verbose {
		extra = append(extra, verboseDecorator)
	}
	client, err := apiclient.BuildClient(apiclient.BuildParams{
		BaseURL:       cfg.BaseURL,
		AuthDecorator: cred.Decorator(),
		Decorators:    extra,
		Timeout:       cfg.Defaults.Timeout,
		MaxRetries:    cfg.Defaults.MaxRetries,
	})
	if err != nil {
		return nil, err
	}
	if s.readOnly() {
		client = apiclient.NewReadOnly(client)
	}
	return client, nil
}

// verboseDecorator logs each outgoing request line to stderr.
func verboseDecorator(req *http.Request) {
	fmt.Fprintf(os.Stderr, "> %s %s\n", req.Method, req.URL.Redacted())
}

// readOnly reports whether the effective posture for this invocation is
// read-only.
func (s *appState) readOnly() bool {
	return s.cfg().Defaults.ReadOnly && !s.gflags.allowWrites
}

// emit writes a successful result to stdout in the configured format.
func (s *appState) emit(v any) error {
	return output.Emit(v, output.Options{
		Format: s.cfg().Defaults.Format,
		Fields: s.fieldList(),
		Writer: os.Stdout,
		Pretty: s.gflags.pretty,
	})
}

// emitList writes a paginated list result to stdout as a {items, next,
// has_more} envelope in the configured format.
func (s *appState) emitList(items any, info pageInfo) error {
	return output.EmitList(items, info.Next, info.HasMore, output.Options{
		Format: s.cfg().Defaults.Format,
		Fields: s.fieldList(),
		Writer: os.Stdout,
		Pretty: s.gflags.pretty,
	})
}

// fieldList splits the --fields flag into dot paths.
func (s *appState) fieldList() []string {
	if s.gflags.fields == "" {
		return nil
	}
	parts := strings.Split(s.gflags.fields, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ndjson reports whether the caller asked for one record per line, the format
// in which a large result is streamed instead of buffered into one blob.
func (s *appState) ndjson() bool { return s.cfg().Defaults.Format == output.FormatNDJSON }

// timeout returns the resolved request timeout.
func (s *appState) timeout() time.Duration { return s.cfg().Defaults.Timeout }

// cmdContext returns a context bounded by the configured request timeout.
func cmdContext(s *appState) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.timeout())
}

// pageInfo carries the pagination cursor for one page of a listing. Only the
// rules endpoint pages server-side (by rule group); the other listings are
// single-shot, so HasMore stays false and the envelope shape stays uniform.
type pageInfo struct {
	Next    string
	HasMore bool
}

// loadContext separates a setup destination from the active runtime context.
func (s *appState) loadContext() string {
	if s.gflags.setupContext != "" {
		return strings.TrimSpace(s.gflags.setupContext)
	}
	return s.gflags.useContext
}
