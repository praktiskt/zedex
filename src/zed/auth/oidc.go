package auth

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// OIDCProvider signs users in with a generic OpenID Connect provider.
type OIDCProvider struct {
	name         string
	displayName  string
	clientID     string
	clientSecret string
	scopes       []string
	issuer       string

	authorizationEndpoint string
	tokenEndpoint         string
	userInfoEndpoint      string

	// discovery holds the lazily fetched OpenID configuration.
	discovered    oidcConfiguration
	discoverMutex sync.Mutex
}

// oidcConfiguration is the part of the OpenID configuration zedex uses.
type oidcConfiguration struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
}

// NewOIDCProvider builds an OpenID Connect provider from its configuration.
func NewOIDCProvider(cfg ProviderConfig) *OIDCProvider {
	displayName := cfg.DisplayName
	if displayName == "" {
		displayName = cfg.Name
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	return &OIDCProvider{
		name:                  cfg.Name,
		displayName:           displayName,
		clientID:              cfg.ClientID,
		clientSecret:          cfg.ClientSecret,
		scopes:                scopes,
		issuer:                strings.TrimSuffix(cfg.Issuer, "/"),
		authorizationEndpoint: cfg.AuthorizationEndpoint,
		tokenEndpoint:         cfg.TokenEndpoint,
		userInfoEndpoint:      cfg.UserInfoEndpoint,
	}
}

// Name is the unique name of the provider.
func (p *OIDCProvider) Name() string { return p.name }

// DisplayName is the label shown on the login page.
func (p *OIDCProvider) DisplayName() string { return p.displayName }

// configuration returns the endpoints of the provider, discovering them from
// the issuer when they are not configured.
func (p *OIDCProvider) configuration(ctx context.Context) (*oidcConfiguration, error) {
	p.discoverMutex.Lock()
	defer p.discoverMutex.Unlock()

	if p.authorizationEndpoint != "" && p.tokenEndpoint != "" && p.userInfoEndpoint != "" {
		return &oidcConfiguration{
			Issuer:                p.issuer,
			AuthorizationEndpoint: p.authorizationEndpoint,
			TokenEndpoint:         p.tokenEndpoint,
			UserInfoEndpoint:      p.userInfoEndpoint,
		}, nil
	}

	if p.discovered.AuthorizationEndpoint == "" {
		if p.issuer == "" {
			return nil, fmt.Errorf("oidc: no issuer configured")
		}
		endpoint := p.issuer + "/.well-known/openid-configuration"
		var cfg oidcConfiguration
		if err := getJSON(ctx, endpoint, nil, &cfg); err != nil {
			// A failed discovery is not remembered, the next sign in tries
			// again.
			return nil, fmt.Errorf("oidc: discovering %s: %w", endpoint, err)
		}
		if cfg.AuthorizationEndpoint == "" || cfg.TokenEndpoint == "" {
			return nil, fmt.Errorf("oidc: %s did not report the required endpoints", endpoint)
		}
		p.discovered = cfg
	}

	cfg := p.discovered
	if p.authorizationEndpoint != "" {
		cfg.AuthorizationEndpoint = p.authorizationEndpoint
	}
	if p.tokenEndpoint != "" {
		cfg.TokenEndpoint = p.tokenEndpoint
	}
	if p.userInfoEndpoint != "" {
		cfg.UserInfoEndpoint = p.userInfoEndpoint
	}
	if cfg.UserInfoEndpoint == "" {
		return nil, fmt.Errorf("oidc: %s did not report a userinfo endpoint", p.issuer)
	}
	return &cfg, nil
}

// AuthorizeURL builds the URL the browser is sent to.
func (p *OIDCProvider) AuthorizeURL(ctx context.Context, state string, redirectURI string, codeChallenge string) (string, error) {
	cfg, err := p.configuration(ctx)
	if err != nil {
		return "", err
	}

	query := url.Values{}
	query.Set("client_id", p.clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join(p.scopes, " "))
	query.Set("state", state)
	if codeChallenge != "" {
		query.Set("code_challenge", codeChallenge)
		query.Set("code_challenge_method", "S256")
	}
	return cfg.AuthorizationEndpoint + "?" + query.Encode(), nil
}

// Exchange trades an authorization code for the profile of the user.
func (p *OIDCProvider) Exchange(ctx context.Context, code string, redirectURI string, codeVerifier string) (*Profile, error) {
	cfg, err := p.configuration(ctx)
	if err != nil {
		return nil, err
	}

	values := url.Values{}
	values.Set("client_id", p.clientID)
	values.Set("client_secret", p.clientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	values.Set("grant_type", "authorization_code")
	if codeVerifier != "" {
		values.Set("code_verifier", codeVerifier)
	}

	var token struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := postForm(ctx, cfg.TokenEndpoint, values, nil, &token); err != nil {
		return nil, err
	}
	if token.Error != "" {
		return nil, fmt.Errorf("oidc: %s: %s", token.Error, token.ErrorDesc)
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("oidc: the token endpoint returned no access token")
	}

	var user struct {
		Subject           string `json:"sub"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		Picture           string `json:"picture"`
	}
	headers := map[string]string{"Authorization": "Bearer " + token.AccessToken}
	if err := getJSON(ctx, cfg.UserInfoEndpoint, headers, &user); err != nil {
		return nil, err
	}
	if user.Subject == "" {
		return nil, fmt.Errorf("oidc: the userinfo endpoint returned no subject")
	}

	username := user.PreferredUsername
	if username == "" {
		username = user.Email
	}
	if username == "" {
		username = user.Subject
	}

	return &Profile{
		Subject:   user.Subject,
		Username:  username,
		Name:      user.Name,
		Email:     user.Email,
		AvatarURL: user.Picture,
	}, nil
}
