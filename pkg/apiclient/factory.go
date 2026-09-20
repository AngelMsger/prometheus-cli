package apiclient

import (
	"net/url"
	"strings"
	"time"

	"github.com/angelmsger/prometheus-cli/pkg/constants"
	cerrors "github.com/angelmsger/prometheus-cli/pkg/errors"
	"github.com/angelmsger/prometheus-cli/pkg/transport"
)

// BuildParams configures BuildClient.
type BuildParams struct {
	BaseURL string
	// AuthDecorator authenticates every request. It may be nil: a bare
	// Prometheus commonly has no authentication at all.
	AuthDecorator transport.Decorator
	// Decorators are applied after the auth decorator (verbose logging, tenant
	// headers).
	Decorators []transport.Decorator
	Timeout    time.Duration
	MaxRetries int
}

// BuildClient assembles a ready-to-use Client: it normalizes the base URL and
// constructs the retrying HTTP transport carrying the auth decorator.
func BuildClient(p BuildParams) (Client, error) {
	if p.BaseURL == "" {
		return nil, cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL",
			"no Prometheus server URL configured").
			WithNextSteps("prometheus-cli config init",
				"Set PROMETHEUS_URL or pass --base-url (e.g. "+constants.DefaultLocalBaseURL+").")
	}
	base, err := NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return nil, err
	}

	decorators := make([]transport.Decorator, 0, len(p.Decorators)+1)
	if p.AuthDecorator != nil {
		decorators = append(decorators, p.AuthDecorator)
	}
	decorators = append(decorators, p.Decorators...)

	tc := transport.New(transport.Options{
		Timeout:    p.Timeout,
		MaxRetries: p.MaxRetries,
		Decorators: decorators,
	})
	return New(Config{BaseURL: base, Transport: tc}), nil
}

// NormalizeBaseURL trims a trailing slash and supplies a scheme when the user
// gave a bare host:port (the common self-hosted case). A URL that already ends
// in the API prefix is trimmed back to the server root, because pasting the
// endpoint from a browser or a runbook is the most common way to get a 404 out
// of an otherwise correct setup.
func NormalizeBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", cerrors.New(cerrors.CategoryConfig, "NO_BASE_URL", "empty base URL")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", cerrors.Newf(cerrors.CategoryConfig, "BAD_BASE_URL",
			"could not parse base URL %q", raw).
			WithHint("Use a form like " + constants.DefaultLocalBaseURL + " or https://prometheus.example.com.")
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), constants.APIPrefix)
	return strings.TrimRight(u.String(), "/"), nil
}
