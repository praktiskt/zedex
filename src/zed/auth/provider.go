package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Profile is the identity a provider reports for a user.
type Profile struct {
	// Provider is the name of the provider that reported the identity.
	Provider string

	// Subject is the stable identifier the provider knows the user by. It is
	// what the numeric user id is derived from.
	Subject string

	// Username is the login the user goes by.
	Username string

	// Name is the display name of the user, if the provider reports one.
	Name string

	// Email is the address of the user, if the provider reports one.
	Email string

	// AvatarURL is the picture of the user, if the provider reports one.
	AvatarURL string

	// GithubLogin is set for providers that are backed by a GitHub account.
	GithubLogin string
}

// Provider is an OAuth provider users can sign in with.
type Provider interface {
	// Name is the unique name of the provider.
	Name() string

	// DisplayName is the label shown on the login page.
	DisplayName() string

	// AuthorizeURL builds the URL the browser is sent to in order to start
	// the authorization code flow.
	AuthorizeURL(ctx context.Context, state string, redirectURI string, codeChallenge string) (string, error)

	// Exchange trades an authorization code for the profile of the user.
	Exchange(ctx context.Context, code string, redirectURI string, codeVerifier string) (*Profile, error)
}

// httpClient is the client used to talk to the providers. It is a variable so
// that tests can point it at a local server.
var httpClient = &http.Client{}

// postForm sends a form encoded request and decodes the JSON response.
func postForm(ctx context.Context, endpoint string, values url.Values, headers map[string]string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return sendJSON(req, out)
}

// getJSON sends a GET request and decodes the JSON response.
func getJSON(ctx context.Context, endpoint string, headers map[string]string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return sendJSON(req, out)
}

// sendJSON performs the request and decodes the response body.
func sendJSON(req *http.Request, out interface{}) error {
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: unexpected status %d: %s", req.URL.Redacted(), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: parsing response: %w", req.URL.Redacted(), err)
	}
	return nil
}
