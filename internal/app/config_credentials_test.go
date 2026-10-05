package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angelmsger/prometheus-cli/internal/auth"
	"github.com/angelmsger/prometheus-cli/internal/config"
	"github.com/zalando/go-keyring"
)

// A stored secret is keyed by the server's host, path and scheme, so every
// context on one deployment shares it — a team preset beside a personal
// context, or two spellings of the same URL. The sibling wecom-calendar-cli
// lost credentials to a cleanup that compared URLs as strings: re-running the
// wizard across a spelling difference deleted the secret it had just saved,
// and removing one context deleted the secret another still resolved.
//
// This CLI has no `config delete-context`, and `config init` never removes a
// secret: the only deletion is an explicit `auth logout`. These tests pin that
// for the one command that rewrites a context, so a future orphan cleanup has
// to keep both guarantees (port forgetUnusedCredential from the sibling, which
// compares account keys rather than URLs).

// gatewayForTest stands in for a Prometheus behind a bearer-checking gateway.
func gatewayForTest(t *testing.T, token string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"3.1.0"}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

// runConfigInit drives the real `config init` line wizard in-process: answers
// arrive on stdin exactly as a piped setup would send them, and neither the
// developer's environment nor a stray .env can reach the loader.
func runConfigInit(t *testing.T, cfgDir, contextName, answers string) {
	t.Helper()
	for _, name := range []string{
		"PROMETHEUS_URL", "PROMETHEUS_AUTH_SCHEME", "PROMETHEUS_CREDENTIAL_URL", "PROMETHEUS_USER",
		"PROMETHEUS_TENANT", "PROMETHEUS_FORMAT", "PROMETHEUS_PASSWORD", "PROMETHEUS_TOKEN",
		"PROMETHEUS_CLI_READ_ONLY", envContextOverride,
	} {
		t.Setenv(name, "")
	}
	t.Chdir(t.TempDir())

	stdin, feed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := feed.WriteString(answers); err != nil {
		t.Fatal(err)
	}
	feed.Close()
	sink, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	prevIn, prevOut, prevErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = stdin, sink, sink
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = prevIn, prevOut, prevErr
		stdin.Close()
		sink.Close()
	}()

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"--config", cfgDir, "config", "init", "--context", contextName})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("config init: %v", err)
	}
}

// resolveStored loads a context the way a later process would and resolves its
// credential from a new store instance.
func resolveStored(t *testing.T, cfgDir, contextName string) (config.Config, auth.Credential) {
	t.Helper()
	fresh, err := config.Load(config.LoadOptions{
		ConfigDir: cfgDir, Context: contextName, DotenvPath: filepath.Join(t.TempDir(), "absent"),
	})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := auth.Resolve(fresh.Config, config.Secrets{}, auth.NewStore(cfgDir))
	if err != nil {
		t.Fatalf("context %q lost its credential: %v", contextName, err)
	}
	return fresh.Config, cred
}

// Re-running the wizard over a context whose URL is merely spelled differently
// must leave the credential it has just saved in place. Both prior spellings
// share the edited URL's account key, which is the case a string comparison
// mistakes for a move to another server.
func TestConfigInitKeepsTheCredentialWhenOnlyTheURLSpellingChanges(t *testing.T) {
	server := gatewayForTest(t, "new-token")
	for name, prior := range map[string]string{
		"trailing slash":    server.URL + "/",
		"upper-case scheme": "HTTP" + strings.TrimPrefix(server.URL, "http"),
	} {
		t.Run(name, func(t *testing.T) {
			keyring.MockInit()
			if auth.AccountKey(prior, auth.SchemeBearer) != auth.AccountKey(server.URL, auth.SchemeBearer) {
				t.Fatalf("fixture %q is not another spelling of %q", prior, server.URL)
			}
			cfgDir := t.TempDir()
			existing := config.File{CurrentContext: "default", Contexts: []config.NamedContext{
				{Name: "default", BaseURL: prior, Auth: config.AuthConfig{Scheme: auth.SchemeBearer}},
			}}
			if err := config.WriteFile(cfgDir, existing); err != nil {
				t.Fatal(err)
			}
			if _, err := auth.Save(prior, auth.Credential{Scheme: auth.SchemeBearer, Secret: "old-token"}, auth.NewStore(cfgDir)); err != nil {
				t.Fatal(err)
			}

			// Server URL, auth scheme, tenant id (none), bearer token.
			runConfigInit(t, cfgDir, "default", server.URL+"\nbearer\n\nnew-token\n")

			cfg, cred := resolveStored(t, cfgDir, "default")
			if cfg.BaseURL != server.URL || cred.Secret != "new-token" {
				t.Fatalf("the wizard deleted or misplaced the credential it had just saved: url=%q secret set=%v",
					cfg.BaseURL, cred.Secret != "")
			}
		})
	}
}

// Moving one context to another server must not take the old server's secret
// away from a second context that still resolves it.
func TestConfigInitKeepsACredentialAnotherContextStillUses(t *testing.T) {
	keyring.MockInit()
	server := gatewayForTest(t, "new-token")
	const shared = "https://shared.example.test/prom"
	cfgDir := t.TempDir()
	existing := config.File{CurrentContext: "default", Contexts: []config.NamedContext{
		{Name: "default", BaseURL: shared + "/", Auth: config.AuthConfig{Scheme: auth.SchemeBearer}},
		// A team preset on the same deployment shares the personal context's secret.
		{Name: "team", BaseURL: shared, Auth: config.AuthConfig{Scheme: auth.SchemeBearer}},
	}}
	if err := config.WriteFile(cfgDir, existing); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Save(shared, auth.Credential{Scheme: auth.SchemeBearer, Secret: "shared-token"}, auth.NewStore(cfgDir)); err != nil {
		t.Fatal(err)
	}

	runConfigInit(t, cfgDir, "default", server.URL+"\nbearer\n\nnew-token\n")

	if cfg, cred := resolveStored(t, cfgDir, "default"); cfg.BaseURL != server.URL || cred.Secret != "new-token" {
		t.Fatalf("the moved context cannot resolve its new credential: url=%q", cfg.BaseURL)
	}
	if cfg, cred := resolveStored(t, cfgDir, "team"); cfg.BaseURL != shared || cred.Secret != "shared-token" {
		t.Fatalf("moving one context removed the credential another still uses: url=%q", cfg.BaseURL)
	}
	after, _, err := config.ReadFile(cfgDir)
	if err != nil || len(after.Contexts) != 2 {
		t.Fatalf("unexpected contexts: %+v (%v)", after, err)
	}
}
