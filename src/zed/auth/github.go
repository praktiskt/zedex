package auth

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// GitHubProvider signs users in with a GitHub OAuth application.
type GitHubProvider struct {
	name         string
	displayName  string
	clientID     string
	clientSecret string
	scopes       []string
}

// NewGitHubProvider builds a GitHub provider from its configuration.
func NewGitHubProvider(cfg ProviderConfig) *GitHubProvider {
	displayName := cfg.DisplayName
	if displayName == "" {
		displayName = "GitHub"
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"read:user", "user:email"}
	}
	return &GitHubProvider{
		name:         cfg.Name,
		displayName:  displayName,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		scopes:       scopes,
	}
}

// Name is the unique name of the provider.
func (p *GitHubProvider) Name() string { return p.name }

// DisplayName is the label shown on the login page.
func (p *GitHubProvider) DisplayName() string { return p.displayName }

// AuthorizeURL builds the URL the browser is sent to.
func (p *GitHubProvider) AuthorizeURL(ctx context.Context, state string, redirectURI string, codeChallenge string) (string, error) {
	query := url.Values{}
	query.Set("client_id", p.clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(p.scopes, " "))
	query.Set("state", state)
	query.Set("allow_signup", "true")
	if codeChallenge != "" {
		query.Set("code_challenge", codeChallenge)
		query.Set("code_challenge_method", "S256")
	}
	return "https://github.com/login/oauth/authorize?" + query.Encode(), nil
}

// Exchange trades an authorization code for the profile of the user.
func (p *GitHubProvider) Exchange(ctx context.Context, code string, redirectURI string, codeVerifier string) (*Profile, error) {
	values := url.Values{}
	values.Set("client_id", p.clientID)
	values.Set("client_secret", p.clientSecret)
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	if codeVerifier != "" {
		values.Set("code_verifier", codeVerifier)
	}

	var token struct {
		AccessToken      string `json:"access_token"`
		TokenType        string `json:"token_type"`
		Scope            string `json:"scope"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := postForm(ctx, "https://github.com/login/oauth/access_token", values, nil, &token); err != nil {
		return nil, err
	}
	if token.Error != "" {
		return nil, fmt.Errorf("github: %s: %s", token.Error, token.ErrorDescription)
	}
	if token.AccessToken == "" {
		return nil, fmt.Errorf("github: the token endpoint returned no access token")
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	headers := map[string]string{
		"Authorization": "Bearer " + token.AccessToken,
		"Accept":        "application/vnd.github+json",
		"User-Agent":    "zedex",
	}
	if err := getJSON(ctx, "https://api.github.com/user", headers, &user); err != nil {
		return nil, err
	}
	if user.Login == "" {
		return nil, fmt.Errorf("github: the user endpoint returned no login")
	}

	return &Profile{
		Subject:     strconv.FormatInt(user.ID, 10),
		Username:    user.Login,
		Name:        user.Name,
		Email:       user.Email,
		AvatarURL:   user.AvatarURL,
		GithubLogin: user.Login,
	}, nil
}
