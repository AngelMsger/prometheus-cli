package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
// `config init` never removes a secret. `config delete-context` removes the
// deleted context's secret only when no remaining context resolves the same
// account key (host, path and scheme), and only after the config write;
// otherwise only an explicit `auth logout` deletes one. These tests pin both
// guarantees for the commands that rewrite or remove a context.

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
		"PROMETHEUS_CLI_READ_ONLY", "PROMETHEUS_CONTEXT",
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

// storedSecret reads the secret stored for a server and scheme.
func storedSecret(t *testing.T, cfgDir, server, scheme string) (string, error) {
	t.Helper()
	return auth.NewStore(cfgDir).Load(auth.AccountKey(server, scheme))
}

func saveSecret(t *testing.T, cfgDir, server, scheme, secret string) {
	t.Helper()
	if _, err := auth.Save(server, auth.Credential{Scheme: scheme, Username: "alice", Secret: secret}, auth.NewStore(cfgDir)); err != nil {
		t.Fatal(err)
	}
}

// Deleting a context removes its secret only when no remaining context
// resolves the same account. Keys include the URL path, so another spelling
// of the same deployment keeps the secret, while a deployment under another
// path on the same host does not.
func TestDeleteContextKeepsACredentialAnotherContextUses(t *testing.T) {
	cfgDir := isolateConfigCommands(t)
	const prom, other = "https://gw.example.test/prom", "https://gw.example.test/other"
	writeContexts(t, cfgDir, "default",
		bearerContext("default", prom+"/"),
		bearerContext("team", prom),
		bearerContext("elsewhere", other))
	saveSecret(t, cfgDir, prom+"/", auth.SchemeBearer, "shared-token")
	saveSecret(t, cfgDir, other, auth.SchemeBearer, "other-token")

	if _, err := runConfig(t, cfgDir, "config", "delete-context", "team"); err != nil {
		t.Fatal(err)
	}
	if got, err := storedSecret(t, cfgDir, prom, auth.SchemeBearer); err != nil || got != "shared-token" {
		t.Fatalf("deleting the preset removed the personal context's credential: %q %v", got, err)
	}
	if _, err := runConfig(t, cfgDir, "config", "delete-context", "elsewhere"); err != nil {
		t.Fatal(err)
	}
	if got, err := storedSecret(t, cfgDir, other, auth.SchemeBearer); !errors.Is(err, auth.ErrSecretNotFound) {
		t.Fatalf("a context on the same host kept another path's credential alive: %q %v", got, err)
	}
	if got, err := storedSecret(t, cfgDir, prom, auth.SchemeBearer); err != nil || got != "shared-token" {
		t.Fatalf("deleting another path's context removed this one's credential: %q %v", got, err)
	}
	if file, _, _ := config.ReadFile(cfgDir); len(file.Contexts) != 1 || file.Contexts[0].Name != "default" {
		t.Fatalf("unexpected contexts: %+v", file)
	}
}

// A context on the same deployment under another scheme keeps its own secret
// and does not keep the deleted context's secret alive. A preset with no
// scheme of its own resolves to `none`, which uses no secret at all.
func TestDeleteContextScopesTheSharedCredentialByScheme(t *testing.T) {
	cfgDir := isolateConfigCommands(t)
	const prom = "https://gw.example.test/prom"
	basic := config.NamedContext{Name: "basic", BaseURL: prom, Auth: config.AuthConfig{Scheme: config.SchemeBasic, Username: "alice"}}
	writeContexts(t, cfgDir, "basic", basic, bearerContext("bearer", prom), config.NamedContext{Name: "preset", BaseURL: prom})
	saveSecret(t, cfgDir, prom, auth.SchemeBearer, "the-token")
	saveSecret(t, cfgDir, prom, auth.SchemeBasic, "the-password")

	if _, err := runConfig(t, cfgDir, "config", "delete-context", "bearer"); err != nil {
		t.Fatal(err)
	}
	if got, err := storedSecret(t, cfgDir, prom, auth.SchemeBearer); !errors.Is(err, auth.ErrSecretNotFound) {
		t.Fatalf("the deleted context's secret was left behind: %q %v", got, err)
	}
	if got, err := storedSecret(t, cfgDir, prom, auth.SchemeBasic); err != nil || got != "the-password" {
		t.Fatalf("a secret under the other scheme was deleted: %q %v", got, err)
	}
}

// A `none` context never stored a secret, so deleting one leaves the store
// alone, even an entry that happens to sit under its key.
func TestDeleteContextLeavesTheStoreAloneForNone(t *testing.T) {
	cfgDir := isolateConfigCommands(t)
	const prom = "https://gw.example.test/prom"
	writeContexts(t, cfgDir, "plain", config.NamedContext{Name: "plain", BaseURL: prom}, bearerContext("other", "https://o.example.test"))
	store := auth.NewStore(cfgDir)
	if _, err := store.Save(auth.AccountKey(prom, auth.SchemeNone), "planted"); err != nil {
		t.Fatal(err)
	}
	if _, err := runConfig(t, cfgDir, "config", "delete-context", "plain"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(auth.AccountKey(prom, auth.SchemeNone)); err != nil || got != "planted" {
		t.Fatalf("deleting a none context touched the store: %q %v", got, err)
	}
}

// A failed config write must not cost the credential: the context is still in
// the file and still resolves it.
func TestDeleteContextKeepsTheCredentialWhenTheConfigWriteFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a read-only directory does not block file creation for this user")
	}
	cfgDir := isolateConfigCommands(t)
	writeContexts(t, cfgDir, "default", bearerContext("default", "https://gw.example.test/prom"), bearerContext("target", "https://target.example.test"))
	saveSecret(t, cfgDir, "https://target.example.test", auth.SchemeBearer, "target-token")
	// The config is replaced through a temporary file in the same directory,
	// so a read-only directory fails the write while the read still succeeds.
	if err := os.Chmod(cfgDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) })
	if _, err := runConfig(t, cfgDir, "config", "delete-context", "target"); err == nil {
		t.Fatal("a failed config write was reported as success")
	}
	if got, err := storedSecret(t, cfgDir, "https://target.example.test", auth.SchemeBearer); err != nil || got != "target-token" {
		t.Fatalf("a failed delete removed a credential the context still names: %q %v", got, err)
	}
}
