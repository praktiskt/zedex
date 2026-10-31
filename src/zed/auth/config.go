// Package auth implements zedex's user authentication.
//
// Zed identifies a user by a numeric id that its API calls the "legacy user
// id". zedex derives that id from an external identity, the subject an OAuth
// provider reports for a user, so that the same person keeps the same id
// across restarts and across servers without zedex having to store anything.
//
// Clients prove who they are with an access token that zedex hands out at the
// end of the OAuth flow. The token carries the user's profile, so the server
// can answer "/client/users/me" and tell clients who their collaborators are
// without keeping any state.
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	// ProviderTypeGitHub selects the GitHub OAuth application flow.
	ProviderTypeGitHub = "github"
	// ProviderTypeOIDC selects a generic OpenID Connect provider.
	ProviderTypeOIDC = "oidc"
)

// Config describes how zedex authenticates its users.
type Config struct {
	// PublicURL is the address clients and OAuth providers use to reach
	// zedex. The OAuth redirect URI is derived from it, so it has to match
	// the URI registered with the provider. When it is empty the address the
	// request came in on is used instead.
	PublicURL string `json:"public_url"`

	// IdentitySecret derives the numeric user id from a provider identity.
	// Changing it changes every user id.
	IdentitySecret string `json:"identity_secret"`

	// SessionSecret signs the access tokens handed to clients. Changing it
	// invalidates every access token but keeps the user ids.
	SessionSecret string `json:"session_secret"`

	// TokenTTLSeconds is how long an access token stays valid. Zero means
	// that tokens never expire, in which case only rotating the session
	// secret invalidates them.
	TokenTTLSeconds int64 `json:"token_ttl_seconds"`

	// AllowGuest keeps the anonymous sign-in available next to the OAuth
	// providers. It defaults to true, so a user who does not want to hand
	// over an account can always sign in without one. Set it to false to
	// require an account.
	AllowGuest *bool `json:"allow_guest"`

	// Providers are the OAuth providers users can sign in with.
	Providers []ProviderConfig `json:"providers"`
}

// ProviderConfig describes one OAuth provider.
type ProviderConfig struct {
	// Name identifies the provider. It appears in the configuration and in
	// the login page, and it has to be unique.
	Name string `json:"name"`

	// Type selects the implementation, either "github" or "oidc".
	Type string `json:"type"`

	// DisplayName is the label shown on the login page. It defaults to the
	// name.
	DisplayName string `json:"display_name"`

	// ClientID and ClientSecret are the credentials of the OAuth
	// application. Both may be given as "env:NAME" to read the value from
	// the environment.
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`

	// Scopes are the OAuth scopes to request.
	Scopes []string `json:"scopes"`

	// Issuer is the OIDC issuer URL. It is only used by the "oidc" type,
	// which discovers its endpoints from the issuer's well known
	// configuration.
	Issuer string `json:"issuer"`

	// AuthorizationEndpoint, TokenEndpoint and UserInfoEndpoint override
	// the endpoints found through OIDC discovery.
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`

	// Primary marks the provider whose logins are used as usernames as
	// they are. The logins of every other provider get a provider suffix
	// appended, which keeps usernames unique when several providers are
	// configured.
	Primary bool `json:"primary"`
}

// LoadConfig reads and validates a configuration file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading auth config: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing auth config: %w", err)
	}

	if err := cfg.resolve(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// resolve reads the environment references in the configuration and checks
// that the configuration is usable.
func (c *Config) resolve() error {
	var err error
	if c.IdentitySecret, err = resolveSecret(c.IdentitySecret, "identity_secret"); err != nil {
		return err
	}
	if c.SessionSecret, err = resolveSecret(c.SessionSecret, "session_secret"); err != nil {
		return err
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.ClientID, err = resolveSecret(p.ClientID, "client_id"); err != nil {
			return err
		}
		if p.ClientSecret, err = resolveSecret(p.ClientSecret, "client_secret"); err != nil {
			return err
		}
	}
	return c.Validate()
}

// resolveSecret reads a secret that is given as "env:NAME" from the
// environment. Everything else is used as it is.
func resolveSecret(value string, field string) (string, error) {
	if !strings.HasPrefix(value, "env:") {
		return value, nil
	}
	name := strings.TrimPrefix(value, "env:")
	resolved := os.Getenv(name)
	if resolved == "" {
		return "", fmt.Errorf("%s: environment variable %s is not set", field, name)
	}
	return resolved, nil
}

// Validate reports whether the configuration can be used to sign users in.
func (c *Config) Validate() error {
	if c.IdentitySecret == "" {
		return fmt.Errorf("identity_secret is required")
	}
	if c.SessionSecret == "" {
		return fmt.Errorf("session_secret is required")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("at least one provider is required")
	}

	seen := map[string]bool{}
	primaries := 0
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == "" {
			return fmt.Errorf("provider %d: name is required", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("provider %q: duplicate name", p.Name)
		}
		seen[p.Name] = true

		switch p.Type {
		case ProviderTypeGitHub:
			if p.ClientID == "" {
				return fmt.Errorf("provider %q: client_id is required", p.Name)
			}
			if p.ClientSecret == "" {
				return fmt.Errorf("provider %q: client_secret is required", p.Name)
			}
		case ProviderTypeOIDC:
			if p.Issuer == "" && p.AuthorizationEndpoint == "" {
				return fmt.Errorf("provider %q: issuer is required", p.Name)
			}
			if p.ClientID == "" {
				return fmt.Errorf("provider %q: client_id is required", p.Name)
			}
			if p.ClientSecret == "" {
				return fmt.Errorf("provider %q: client_secret is required", p.Name)
			}
			if p.Issuer == "" && p.UserInfoEndpoint == "" {
				return fmt.Errorf("provider %q: userinfo_endpoint is required", p.Name)
			}
		default:
			return fmt.Errorf("provider %q: unknown type %q", p.Name, p.Type)
		}

		if p.Primary {
			primaries++
		}
	}
	if primaries > 1 {
		return fmt.Errorf("only one provider can be primary")
	}
	return nil
}

// PrimaryProvider returns the name of the provider whose logins are used as
// usernames as they are. When no provider is marked as primary the first one
// is used.
func (c *Config) PrimaryProvider() string {
	for _, p := range c.Providers {
		if p.Primary {
			return p.Name
		}
	}
	if len(c.Providers) > 0 {
		return c.Providers[0].Name
	}
	return ""
}

// GuestsAllowed reports whether users may sign in without an account.
func (c *Config) GuestsAllowed() bool {
	return c.AllowGuest == nil || *c.AllowGuest
}

// RedirectURI returns the URI the OAuth provider sends the user back to.
func (c *Config) RedirectURI() string {
	return strings.TrimSuffix(c.PublicURL, "/") + "/oauth/callback"
}
