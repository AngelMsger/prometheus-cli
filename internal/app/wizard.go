package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/angelmsger/prometheus-cli/internal/auth"
	"github.com/angelmsger/prometheus-cli/internal/config"
	"github.com/angelmsger/prometheus-cli/pkg/constants"
	"github.com/charmbracelet/huh"
)

// initValues are the inputs collected by the config-init flow, whether through
// the interactive TUI (--pretty) or plain line prompts.
type initValues struct {
	credentialURL string
	baseURL       string
	scheme        string
	username      string
	tenant        string
	secret        string // password (basic) or bearer token (bearer)
}

// withDefaults seeds empty fields with sensible defaults so the wizard shows
// them pre-filled.
func (v initValues) withDefaults() initValues {
	if v.baseURL == "" {
		v.baseURL = constants.DefaultLocalBaseURL
	}
	if v.scheme == "" {
		v.scheme = auth.SchemeNone
	}
	return v
}

// runInitForm presents the interactive huh TUI to collect config-init values,
// pre-seeded with def. The form renders to stderr (and reads stdin), so the
// command's JSON result on stdout stays a clean data channel.
func runInitForm(def initValues) (initValues, error) {
	v := def.withDefaults()
	serviceForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server URL").
				Placeholder(constants.DefaultLocalBaseURL).
				Value(&v.baseURL).
				Validate(required),
			huh.NewSelect[string]().
				Title("Auth scheme").
				Options(
					huh.NewOption("none — no authentication (a plain Prometheus)", auth.SchemeNone),
					huh.NewOption("bearer — a token from a hosted or proxied endpoint", auth.SchemeBearer),
					huh.NewOption("basic — username + password from a gateway", auth.SchemeBasic),
				).
				Value(&v.scheme),
			huh.NewInput().
				Title("Tenant id (optional)").
				Description("Sent as X-Scope-OrgID. Required by Cortex, Mimir and Thanos Receive; leave empty for Prometheus.").
				Value(&v.tenant),
		),
	).WithInput(os.Stdin).WithOutput(os.Stderr)
	if err := serviceForm.Run(); err != nil {
		return v, err
	}
	// The `none` scheme has no username and no secret to collect, so the
	// credential half of the wizard is skipped entirely.
	if v.scheme == auth.SchemeNone {
		return v, nil
	}
	guidance, err := wizardGuide(v)
	if err != nil {
		return v, err
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Username").
				Value(&v.username).
				Validate(required),
		).WithHideFunc(func() bool { return v.scheme != auth.SchemeBasic }),
		huh.NewGroup(
			huh.NewInput().
				Title("Bearer token").
				Description(strings.Join(guidance.Lines(), "\n")).
				EchoMode(huh.EchoModePassword).
				Value(&v.secret).
				Validate(required),
		).WithHideFunc(func() bool { return v.scheme != auth.SchemeBearer }),
		huh.NewGroup(
			huh.NewInput().
				Title("Password").
				Description(strings.Join(guidance.Lines(), "\n")).
				EchoMode(huh.EchoModePassword).
				Value(&v.secret).
				Validate(required),
		).WithHideFunc(func() bool { return v.scheme != auth.SchemeBasic }),
	).WithInput(os.Stdin).WithOutput(os.Stderr)

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return v, fmt.Errorf("setup cancelled")
		}
		return v, err
	}
	return v, nil
}

// runInitPrompts collects the same values through plain line prompts (stderr),
// used when --pretty is not set.
func runInitPrompts(def initValues) (initValues, error) {
	v := def.withDefaults()
	var err error
	if v.baseURL, err = promptLine("Server URL", v.baseURL); err != nil {
		return v, err
	}
	if v.scheme, err = promptChoice("Auth scheme (none/bearer/basic)", config.Schemes(), v.scheme); err != nil {
		return v, err
	}
	if v.tenant, err = promptLine("Tenant id (X-Scope-OrgID, blank for none)", v.tenant); err != nil {
		return v, err
	}
	if v.scheme == auth.SchemeNone {
		return v, nil
	}
	if g, e := wizardGuide(v); e != nil {
		return v, e
	} else {
		for _, line := range g.Lines() {
			fmt.Fprintln(os.Stderr, line)
		}
	}
	label := "Bearer token"
	if v.scheme == auth.SchemeBasic {
		label = "Password"
		if v.username, err = promptLine("Username", v.username); err != nil {
			return v, err
		}
	}
	if v.secret, err = promptSecret(label); err != nil {
		return v, err
	}
	return v, nil
}

func required(s string) error {
	if s == "" {
		return errors.New("required")
	}
	return nil
}

// formSelect runs a single-select huh form (rendered to stderr, reading stdin),
// used by `config init` to ask the edit/add/replace action or which context to
// edit. It returns the selected option value.
func formSelect(title string, options []huh.Option[string], def string) (string, error) {
	val := def
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().Title(title).Options(options...).Value(&val),
		),
	).WithInput(os.Stdin).WithOutput(os.Stderr)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", fmt.Errorf("setup cancelled")
		}
		return "", err
	}
	return val, nil
}

// formInput runs a single free-text huh input (e.g. a new context name).
func formInput(title, placeholder string) (string, error) {
	var val string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title(title).Placeholder(placeholder).Value(&val),
		),
	).WithInput(os.Stdin).WithOutput(os.Stderr)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", fmt.Errorf("setup cancelled")
		}
		return "", err
	}
	return val, nil
}

func wizardGuide(v initValues) (config.AuthGuide, error) {
	return config.Guide(config.Config{
		BaseURL: v.baseURL,
		Auth: config.AuthConfig{
			Scheme:        v.scheme,
			CredentialURL: v.credentialURL,
			TenantID:      v.tenant,
		},
	}, nil)
}
