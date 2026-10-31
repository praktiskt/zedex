package zed

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"zedex/zed/auth"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider is an OAuth provider that hands out a fixed profile.
type fakeProvider struct {
	name    string
	profile *auth.Profile
	// exchanges counts how often a code was exchanged.
	exchanges int
}

func (p *fakeProvider) Name() string        { return p.name }
func (p *fakeProvider) DisplayName() string { return "Fake Provider" }

func (p *fakeProvider) AuthorizeURL(ctx context.Context, state string, redirectURI string, codeChallenge string) (string, error) {
	query := url.Values{}
	query.Set("state", state)
	query.Set("redirect_uri", redirectURI)
	query.Set("code_challenge", codeChallenge)
	return "https://provider.example.com/authorize?" + query.Encode(), nil
}

func (p *fakeProvider) Exchange(ctx context.Context, code string, redirectURI string, codeVerifier string) (*auth.Profile, error) {
	p.exchanges++
	if code != "the-code" {
		return nil, fmt.Errorf("the provider refused the code %q", code)
	}
	profile := *p.profile
	return &profile, nil
}

// testKeyPair returns a key pair in the shape Zed uses when it signs in: the
// public key is the base64 of the PKCS1 DER, the private key decrypts the
// access token.
func testKeyPair(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der := x509.MarshalPKCS1PublicKey(&key.PublicKey)
	return base64.URLEncoding.EncodeToString(der), key
}

// decryptToken decrypts an access token the way Zed does.
func decryptToken(t *testing.T, key *rsa.PrivateKey, encrypted string) string {
	t.Helper()

	raw, err := base64.URLEncoding.DecodeString(encrypted)
	require.NoError(t, err)

	plaintext, err := rsa.DecryptOAEP(newSHA256(), rand.Reader, key, raw, nil)
	require.NoError(t, err)
	return string(plaintext)
}

// testAuthManager builds a manager with a fake provider.
func testAuthManager(t *testing.T, provider *fakeProvider) *auth.Manager {
	t.Helper()

	manager, err := auth.New(&auth.Config{
		IdentitySecret: "identity-secret",
		SessionSecret:  "session-secret",
		PublicURL:      "https://zedex.example.com",
		Providers: []auth.ProviderConfig{
			{
				Name:         "fake",
				Type:         auth.ProviderTypeGitHub,
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				Primary:      true,
			},
		},
	})
	require.NoError(t, err)
	manager.Register(provider)
	return manager
}

// startAuthTestServer starts a server with authentication turned on.
func startAuthTestServer(t *testing.T, manager *auth.Manager) *httptest.Server {
	t.Helper()

	gin.SetMode(gin.TestMode)
	api := NewAPI(Options{
		EnableLogin:        true,
		EnableReleases:     true,
		EnableReleaseNotes: true,
		Client:             NewZedClient(1),
		Auth:               manager,
	})
	server := httptest.NewServer(api.Router())
	t.Cleanup(server.Close)
	return server
}

// TestSignInWithProvider walks through the whole sign in: Zed opens the
// login page, the browser picks a provider, the provider sends the user
// back and Zed ends up with an id and a token it can trust.
func TestSignInWithProvider(t *testing.T) {
	provider := &fakeProvider{
		name: "fake",
		profile: &auth.Profile{
			Subject:     "4711",
			Username:    "magnus",
			Name:        "Magnus Smith",
			Email:       "magnus@example.com",
			AvatarURL:   "https://provider.example.com/avatar.png",
			GithubLogin: "magnus",
		},
	}
	manager := testAuthManager(t, provider)
	server := startAuthTestServer(t, manager)

	publicKey, privateKey := testKeyPair(t)

	// Zed opens the login page.
	page := getWithRedirects(t, server, fmt.Sprintf(
		"/native_app_signin?native_app_port=1234&native_app_public_key=%s", url.QueryEscape(publicKey)))
	require.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, "Continue with Fake Provider")
	assert.Contains(t, page.body, "Continue without an account")

	// The page holds the id of the sign in.
	pendingID := between(page.body, "id=", "&")
	require.NotEmpty(t, pendingID)

	// The browser picks the provider.
	start := getNoRedirect(t, server, "/oauth/start?id="+pendingID+"&provider=fake")
	require.Equal(t, http.StatusFound, start.StatusCode)

	location, err := url.Parse(start.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "provider.example.com", location.Host)
	assert.Equal(t, pendingID, location.Query().Get("state"))
	assert.NotEmpty(t, location.Query().Get("code_challenge"))

	// The provider sends the user back.
	callback := getNoRedirect(t, server, "/oauth/callback?code=the-code&state="+pendingID)
	require.Equal(t, http.StatusFound, callback.StatusCode)
	assert.Equal(t, 1, provider.exchanges)

	target, err := url.Parse(callback.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:1234", target.Host)

	// Zed gets the user id and the access token.
	assert.Equal(t, fmt.Sprintf("%d", manager.DeriveUserID("fake", "4711")), target.Query().Get("user_id"))

	token := decryptToken(t, privateKey, target.Query().Get("access_token"))
	claims, err := manager.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, uint64(manager.DeriveUserID("fake", "4711")), claims.UID)
	assert.Equal(t, "magnus", claims.Username)
	assert.Equal(t, "fake", claims.Provider)
}

// TestSignInWithoutAnAccount checks the anonymous session that is always
// available.
func TestSignInWithoutAnAccount(t *testing.T) {
	provider := &fakeProvider{
		name:    "fake",
		profile: &auth.Profile{Subject: "4711", Username: "magnus"},
	}
	manager := testAuthManager(t, provider)
	server := startAuthTestServer(t, manager)

	publicKey, privateKey := testKeyPair(t)

	page := getWithRedirects(t, server, fmt.Sprintf(
		"/native_app_signin?native_app_port=1234&native_app_public_key=%s", url.QueryEscape(publicKey)))
	require.Equal(t, http.StatusOK, page.status)

	pendingID := between(page.body, "/oauth/guest?id=", "\"")
	require.NotEmpty(t, pendingID)

	guest := getNoRedirect(t, server, "/oauth/guest?id="+pendingID)
	require.Equal(t, http.StatusFound, guest.StatusCode)
	assert.Equal(t, 0, provider.exchanges)

	target, err := url.Parse(guest.Header.Get("Location"))
	require.NoError(t, err)

	token := decryptToken(t, privateKey, target.Query().Get("access_token"))
	claims, err := manager.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, auth.GuestProvider, claims.Provider)
	assert.NotEmpty(t, claims.Username)
	assert.Equal(t, claims.UID, manager.DeriveUserID(auth.GuestProvider, claims.Subject))
}

// TestSignInWithoutAuthentication checks the behaviour of a server that has
// no authentication configured: it hands out an anonymous identity without
// asking anything.
func TestSignInWithoutAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	api := NewAPI(Options{
		EnableLogin:        true,
		EnableReleases:     true,
		EnableReleaseNotes: true,
		Client:             NewZedClient(1),
	})
	server := httptest.NewServer(api.Router())
	t.Cleanup(server.Close)

	publicKey, privateKey := testKeyPair(t)

	response := getNoRedirect(t, server, fmt.Sprintf(
		"/native_app_signin?native_app_port=1234&native_app_public_key=%s", url.QueryEscape(publicKey)))
	require.Equal(t, http.StatusFound, response.StatusCode)

	target, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:1234", target.Host)
	assert.NotEmpty(t, target.Query().Get("user_id"))
	assert.Equal(t, "a", decryptToken(t, privateKey, target.Query().Get("access_token")))
}

// TestAuthenticatedUser checks that the server tells Zed who the caller is
// and that it refuses a token it did not issue.
func TestAuthenticatedUser(t *testing.T) {
	provider := &fakeProvider{
		name:    "fake",
		profile: &auth.Profile{Subject: "4711", Username: "magnus", AvatarURL: "https://provider.example.com/a.png"},
	}
	manager := testAuthManager(t, provider)
	server := startAuthTestServer(t, manager)

	uid, token, err := manager.Mint(&auth.Profile{
		Provider:  "fake",
		Subject:   "4711",
		Username:  "magnus",
		AvatarURL: "https://provider.example.com/a.png",
	})
	require.NoError(t, err)

	response := getWithAuth(t, server, fmt.Sprintf("%d %s", uid, token))
	require.Equal(t, http.StatusOK, response.StatusCode)
	body := readBody(t, response)
	assert.Contains(t, body, `"username":"magnus"`)
	assert.Contains(t, body, `"legacy_user_id":`+fmt.Sprintf("%d", uid))
	assert.Contains(t, body, "https://provider.example.com/a.png")

	// A token the server did not issue is refused.
	response = getWithAuth(t, server, fmt.Sprintf("%d v1.aaaa.bbbb", uid))
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)

	// A token of another user is refused.
	response = getWithAuth(t, server, fmt.Sprintf("%d %s", uid+1, token))
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)

	// A missing token is refused.
	response = getNoRedirect(t, server, "/client/users/me")
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

// TestCollabRejectsAnInvalidToken checks that the collaboration server does
// not accept a connection that cannot be trusted.
func TestCollabRejectsAnInvalidToken(t *testing.T) {
	provider := &fakeProvider{
		name:    "fake",
		profile: &auth.Profile{Subject: "4711", Username: "magnus"},
	}
	manager := testAuthManager(t, provider)

	rpc := NewRpcHandler()
	server := startTestRPCServerWithAuth(t, &rpc, manager)

	// A token the server did not issue.
	status := websocketStatus(t, server, "4711 v1.aaaa.bbbb")
	assert.Equal(t, http.StatusUnauthorized, status)

	// A valid token is accepted.
	uid, token, err := manager.Mint(&auth.Profile{Provider: "fake", Subject: "4711", Username: "magnus"})
	require.NoError(t, err)
	status = websocketStatus(t, server, fmt.Sprintf("%d %s", uid, token))
	assert.Equal(t, http.StatusSwitchingProtocols, status)
}

// pageResult is the result of a request that followed the redirects.
type pageResult struct {
	status int
	body   string
}

// between returns the text between the first occurrence of start and the
// first occurrence of end after it.
func between(text string, start string, end string) string {
	from := strings.Index(text, start)
	if from < 0 {
		return ""
	}
	from += len(start)

	to := strings.Index(text[from:], end)
	if to < 0 {
		return ""
	}
	return text[from : from+to]
}
