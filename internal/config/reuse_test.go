package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/angelmsger/prometheus-cli/internal/auth" // registers the auth schemes for this package's tests
	"github.com/angelmsger/prometheus-cli/internal/config"
)

func TestLoadOffersReuseBeforeSignIn(t *testing.T) {
	dir := t.TempDir()
	for _, k := range []string{"PROMETHEUS_URL", "PROMETHEUS_CREDENTIAL_URL", "PROMETHEUS_AUTH_SCHEME", "PROMETHEUS_USER",
		"PROMETHEUS_TENANT", "PROMETHEUS_TOKEN", "PROMETHEUS_PASSWORD", "PROMETHEUS_CONTEXT"} {
		t.Setenv(k, "")
	}
	const base = "https://p.example.test/prom"
	file := config.File{CurrentContext: "team", Contexts: []config.NamedContext{
		{Name: "personal", BaseURL: base, Auth: config.AuthConfig{Scheme: "basic", Username: "me", TenantID: "team-a"}},
		{Name: "plain", BaseURL: base, Auth: config.AuthConfig{Scheme: "none", TenantID: "team-a"}},
		{Name: "team", BaseURL: base, Auth: config.AuthConfig{Scheme: "bearer", TenantID: "team-a"}},
	}}
	if err := config.WriteFile(dir, file); err != nil {
		t.Fatal(err)
	}
	load := func(opt config.LoadOptions) *config.Resolved {
		t.Helper()
		opt.ConfigDir, opt.DotenvPath = dir, filepath.Join(t.TempDir(), "absent.env")
		r, err := config.Load(opt)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := load(config.LoadOptions{})
	if r.Config.ReuseStep != "prometheus-cli --use-context 'team' auth reuse" {
		t.Fatalf("reuse step %q", r.Config.ReuseStep)
	}
	g, err := config.Guide(r.Config, r.Sources)
	if err != nil || g.NextSteps[0] != r.Config.ReuseStep || !strings.Contains(g.Instructions[0], "auth reuse") {
		t.Fatalf("guide %+v %v", g, err)
	}
	for name, opt := range map[string]config.LoadOptions{
		"identity":        {Context: "personal"},
		"none":            {Context: "plain"},
		"supplied scheme": {Flags: config.FlagValues{AuthScheme: "bearer"}},
		"server override": {Flags: config.FlagValues{BaseURL: "https://p.example.test/other"}},
		"tenant override": {Flags: config.FlagValues{TenantID: "team-b"}},
	} {
		if r = load(opt); r.Config.ReuseStep != "" {
			t.Fatalf("%s was offered reuse: %q", name, r.Config.ReuseStep)
		}
	}
	t.Setenv("PROMETHEUS_USER", "someone")
	if r = load(config.LoadOptions{}); r.Config.ReuseStep != "" {
		t.Fatalf("a runtime username was offered reuse: %q", r.Config.ReuseStep)
	}
}
