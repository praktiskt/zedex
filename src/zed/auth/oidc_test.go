package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeOIDCServer is an OpenID Connect provider that answers the requests
// zedex makes.
type fakeOIDCServer struct {
	server *httptest.Server
	// tokens counts the token requests.
	tokens int
	// userinfo counts the userinfo requests.
	userinfo int
	// code is the code the server accepts.
	code string
	// verifier is the code verifier the server expects.
	verifier string
}

func newFakeOIDCServer(t *testing.T) *fakeOIDCServer {
	t.Helper()

	fake := &fakeOIDCServer{code: "the-code"}
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{
			"issuer":                 fake.server.URL,
			"authorization_endpoint": fake.server.URL + "/authorize",
			"token_endpoint":         fake.server.URL + "/token",
			"userinfo_endpoint":      fake.server.URL + "/userinfo",
		})
	})

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		fake.tokens++
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != fake.code {
			writeJSON(w, map[string]string{"error": "bad code"})
			return
		}
		if r.Form.Get("code_verifier") != fake.verifier {
			writeJSON(w, map[string]string{"error": "bad verifier"})
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			writeJSON(w, map[string]string{"error": "bad grant"})
			return
		}
		writeJSON(w, map[string]string{
			"access_token": "an-access-token",
			"token_type":   "Bearer",
		})
	})

	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		fake.userinfo++
		if r.Header.Get("Authorization") != "Bearer an-access-token" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		writeJSON(w, map[string]string{
			"sub":                "248289761001",
			"name":               "Magnus Smith",
			"preferred_username": "magnus",
			"email":              "magnus@example.com",
			"picture":            "https://issuer.example.com/avatar.png",
		})
	})

	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}

func TestOIDCProvider(t *testing.T) {
	fake := newFakeOIDCServer(t)

	provider := NewOIDCProvider(ProviderConfig{
		Name:         "okta",
		Type:         ProviderTypeOIDC,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Issuer:       fake.server.URL,
	})

	// The authorization URL is built from the discovered endpoint.
	location, err := provider.AuthorizeURL(context.Background(), "the-state", "https://zedex.example.com/oauth/callback", "the-challenge")
	if err != nil {
		t.Fatalf("could not build the authorization url: %v", err)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("could not parse the authorization url: %v", err)
	}
	if parsed.Host != strings.TrimPrefix(fake.server.URL, "http://") {
		t.Errorf("the authorization url points at %q, expected %q", parsed.Host, fake.server.URL)
	}
	query := parsed.Query()
	if query.Get("state") != "the-state" {
		t.Errorf("the authorization url carries the state %q", query.Get("state"))
	}
	if query.Get("redirect_uri") != "https://zedex.example.com/oauth/callback" {
		t.Errorf("the authorization url carries the redirect uri %q", query.Get("redirect_uri"))
	}
	if query.Get("code_challenge") != "the-challenge" {
		t.Errorf("the authorization url carries the code challenge %q", query.Get("code_challenge"))
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Errorf("the authorization url should use S256, got %q", query.Get("code_challenge_method"))
	}

	// The code is exchanged for the profile of the user.
	fake.verifier = "the-verifier"
	profile, err := provider.Exchange(context.Background(), "the-code", "https://zedex.example.com/oauth/callback", "the-verifier")
	if err != nil {
		t.Fatalf("could not exchange the code: %v", err)
	}
	if profile.Subject != "248289761001" {
		t.Errorf("the profile carries the subject %q", profile.Subject)
	}
	if profile.Username != "magnus" {
		t.Errorf("the profile carries the username %q", profile.Username)
	}
	if profile.Name != "Magnus Smith" {
		t.Errorf("the profile carries the name %q", profile.Name)
	}
	if profile.Email != "magnus@example.com" {
		t.Errorf("the profile carries the email %q", profile.Email)
	}
	if profile.AvatarURL != "https://issuer.example.com/avatar.png" {
		t.Errorf("the profile carries the avatar %q", profile.AvatarURL)
	}
	if fake.tokens != 1 || fake.userinfo != 1 {
		t.Errorf("the provider was asked %d times for a token and %d times for the user", fake.tokens, fake.userinfo)
	}

	// The discovery is only done once.
	if _, err := provider.Exchange(context.Background(), "the-code", "https://zedex.example.com/oauth/callback", "the-verifier"); err != nil {
		t.Fatalf("could not exchange the code again: %v", err)
	}
	if fake.tokens != 2 {
		t.Errorf("the provider was asked %d times for a token", fake.tokens)
	}
}

func TestOIDCProviderRejectsABadCode(t *testing.T) {
	fake := newFakeOIDCServer(t)

	provider := NewOIDCProvider(ProviderConfig{
		Name:         "okta",
		Type:         ProviderTypeOIDC,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Issuer:       fake.server.URL,
	})

	if _, err := provider.Exchange(context.Background(), "another-code", "https://zedex.example.com/oauth/callback", ""); err == nil {
		t.Errorf("a bad code should be refused")
	}
}

func TestOIDCProviderRetriesAFailedDiscovery(t *testing.T) {
	// A provider whose issuer cannot be reached should report the error,
	// and try again the next time.
	provider := NewOIDCProvider(ProviderConfig{
		Name:         "okta",
		Type:         ProviderTypeOIDC,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Issuer:       "http://127.0.0.1:1",
	})

	if _, err := provider.AuthorizeURL(context.Background(), "state", "https://zedex.example.com/oauth/callback", "challenge"); err == nil {
		t.Errorf("a provider that cannot be reached should report an error")
	}
	if _, err := provider.AuthorizeURL(context.Background(), "state", "https://zedex.example.com/oauth/callback", "challenge"); err == nil {
		t.Errorf("a provider that cannot be reached should report an error")
	}
}

func TestGitHubAuthorizeURL(t *testing.T) {
	provider := NewGitHubProvider(ProviderConfig{
		Name:         "github",
		Type:         ProviderTypeGitHub,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	})

	location, err := provider.AuthorizeURL(context.Background(), "the-state", "https://zedex.example.com/oauth/callback", "the-challenge")
	if err != nil {
		t.Fatalf("could not build the authorization url: %v", err)
	}
	if !strings.HasPrefix(location, "https://github.com/login/oauth/authorize?") {
		t.Errorf("the authorization url is %q", location)
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("could not parse the authorization url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "client-id" {
		t.Errorf("the authorization url carries the client id %q", query.Get("client_id"))
	}
	if query.Get("state") != "the-state" {
		t.Errorf("the authorization url carries the state %q", query.Get("state"))
	}
	if query.Get("redirect_uri") != "https://zedex.example.com/oauth/callback" {
		t.Errorf("the authorization url carries the redirect uri %q", query.Get("redirect_uri"))
	}
	if !strings.Contains(query.Get("scope"), "read:user") {
		t.Errorf("the authorization url asks for the scope %q", query.Get("scope"))
	}
	if query.Get("code_challenge") != "the-challenge" {
		t.Errorf("the authorization url carries the code challenge %q", query.Get("code_challenge"))
	}
}
