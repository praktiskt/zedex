package zed

import (
	"fmt"
	"html/template"
	"math"
	mrand "math/rand"
	"net/http"
	"strconv"
	"strings"

	"zedex/zed/auth"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// signin.go implements the sign in flow.
//
// Zed signs in by opening the login page of the server in a browser and
// waiting for a callback on a local port. The page offers the configured
// OAuth providers and, when it is allowed, an anonymous session. Whichever
// the user picks, the browser ends up back on the local port carrying the
// user id and an access token that is encrypted with the key Zed sent along.

// loginPage is the data of the login page.
type loginPage struct {
	Providers []loginProvider
	Guest     bool
	PendingID string
}

// loginProvider is one provider button of the login page.
type loginProvider struct {
	Name        string
	DisplayName string
	URL         string
}

// loginPageTemplate is the page Zed opens in the browser to sign in.
var loginPageTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in to Zedex</title>
<style>
	body { background-color: #1e1e2e; color: #ffffff; font-family: sans-serif;
	       display: flex; justify-content: center; align-items: center;
	       min-height: 100vh; margin: 0; }
	main { text-align: center; max-width: 24rem; padding: 2rem; }
	h1 { font-size: 1.4rem; font-weight: 600; margin-bottom: 0.25rem; }
	p { color: #a6adc8; margin-top: 0; }
	a.button { display: block; background-color: #313244; color: #ffffff;
	           text-decoration: none; border-radius: 6px; padding: 0.7rem 1rem;
	           margin-top: 0.75rem; border: 1px solid #45475a; }
	a.button:hover { background-color: #3a3d52; }
	a.secondary { color: #a6adc8; font-size: 0.85rem; display: inline-block;
	              margin-top: 1.5rem; }
</style>
</head>
<body>
<main>
	<h1>Sign in to Zedex</h1>
	<p>Pick an account to continue.</p>
	{{range .Providers}}
	<a class="button" href="{{.URL}}">Continue with {{.DisplayName}}</a>
	{{end}}
	{{if .Guest}}
	<a class="secondary" href="/oauth/guest?id={{.PendingID}}">Continue without an account</a>
	{{end}}
</main>
</body>
</html>
`))

// NativeAppSignin is the page Zed opens in the browser to sign in.
func (co *Controller) NativeAppSignin(c *gin.Context) {
	port := c.Query("native_app_port")
	publicKey := c.Query("native_app_public_key")

	if co.auth == nil {
		// Without an authentication configuration every visitor gets an
		// anonymous identity and no token is checked.
		uid := mrand.Intn(math.MaxInt32)
		co.redirectToZed(c, port, publicKey, uint64(uid), "a")
		return
	}

	pending, err := co.authFlow.Create(port, publicKey, co.redirectURI(c))
	if err != nil {
		logrus.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	page := loginPage{
		Guest:     co.auth.AllowGuest(),
		PendingID: pending.ID,
	}
	for _, provider := range co.auth.Providers() {
		page.Providers = append(page.Providers, loginProvider{
			Name:        provider.Name(),
			DisplayName: provider.DisplayName(),
			URL:         "/oauth/start?id=" + pending.ID + "&provider=" + provider.Name(),
		})
	}

	c.Data(http.StatusOK, "text/html; charset=utf-8", renderLoginPage(page))
}

// renderLoginPage renders the login page.
func renderLoginPage(page loginPage) []byte {
	buf := &strings.Builder{}
	if err := loginPageTemplate.Execute(buf, page); err != nil {
		return []byte("<html><body>Could not render the sign in page.</body></html>")
	}
	return []byte(buf.String())
}

// OAuthStart sends the browser to the provider the user picked.
func (co *Controller) OAuthStart(c *gin.Context) {
	if co.auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
		return
	}

	pendingID := c.Query("id")
	pending := co.authFlow.Get(pendingID)
	if pending == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "the sign in expired, start over",
		})
		return
	}

	provider, ok := co.auth.Provider(c.Query("provider"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": "unknown provider",
		})
		return
	}

	verifier, challenge, err := auth.NewCodeVerifier()
	if err != nil {
		logrus.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
		return
	}
	if err := co.authFlow.Start(pendingID, provider.Name(), verifier); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"message": err.Error(),
		})
		return
	}

	location, err := provider.AuthorizeURL(c.Request.Context(), pending.ID, pending.RedirectURI, challenge)
	if err != nil {
		logrus.Error(err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "Bad Gateway",
			"message": err.Error(),
		})
		return
	}
	c.Redirect(http.StatusFound, location)
}

// OAuthCallback finishes the sign in after the provider sent the user back.
func (co *Controller) OAuthCallback(c *gin.Context) {
	if co.auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
		return
	}

	if failure := c.Query("error"); failure != "" {
		co.renderSigninError(c, fmt.Sprintf("the provider refused the sign in: %s", failure))
		return
	}

	code := c.Query("code")
	if code == "" {
		co.renderSigninError(c, "the provider did not send a code")
		return
	}

	pending := co.authFlow.Take(c.Query("state"))
	if pending == nil {
		co.renderSigninError(c, "the sign in expired, start over")
		return
	}

	provider, ok := co.auth.Provider(pending.Provider)
	if !ok {
		co.renderSigninError(c, "unknown provider")
		return
	}

	profile, err := provider.Exchange(c.Request.Context(), code, pending.RedirectURI, pending.CodeVerifier)
	if err != nil {
		logrus.Error(err)
		co.renderSigninError(c, fmt.Sprintf("could not sign in: %s", err))
		return
	}
	profile.Provider = provider.Name()

	uid, token, err := co.auth.Mint(profile)
	if err != nil {
		logrus.Error(err)
		co.renderSigninError(c, fmt.Sprintf("could not issue a token: %s", err))
		return
	}

	co.redirectToZed(c, pending.Port, pending.PublicKey, uid, token)
}

// OAuthGuest signs the user in anonymously, even when there are providers.
func (co *Controller) OAuthGuest(c *gin.Context) {
	if co.auth == nil || !co.auth.AllowGuest() {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not Found"})
		return
	}

	pending := co.authFlow.Take(c.Query("id"))
	if pending == nil {
		co.renderSigninError(c, "the sign in expired, start over")
		return
	}

	// Every anonymous session gets its own identity.
	subject, err := auth.RandomSubject()
	if err != nil {
		logrus.Error(err)
		co.renderSigninError(c, fmt.Sprintf("could not start a session: %s", err))
		return
	}
	uid := co.auth.DeriveUserID(auth.GuestProvider, subject)
	name, _, _ := identityFor(uid)

	token, err := co.auth.MintGuest(uid, subject, name)
	if err != nil {
		logrus.Error(err)
		co.renderSigninError(c, fmt.Sprintf("could not issue a token: %s", err))
		return
	}

	co.redirectToZed(c, pending.Port, pending.PublicKey, uid, token)
}

// redirectToZed sends the browser back to Zed with the user id and the
// access token, encrypted with the key Zed provided.
func (co *Controller) redirectToZed(c *gin.Context, port string, publicKey string, uid uint64, token string) {
	enc, err := encryptStringV1(publicKey, token)
	if err != nil {
		logrus.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	host := fmt.Sprintf("http://127.0.0.1:%s/native_app_signin?user_id=%v&access_token=%s", port, uid, enc)
	c.Redirect(http.StatusFound, host)
}

// redirectURI returns the URI the OAuth provider sends the user back to.
func (co *Controller) redirectURI(c *gin.Context) string {
	if publicURL := co.auth.Config().PublicURL; publicURL != "" {
		return strings.TrimSuffix(publicURL, "/") + "/oauth/callback"
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := c.GetHeader("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return fmt.Sprintf("%s://%s/oauth/callback", scheme, c.Request.Host)
}

// renderSigninError shows a message in the browser.
func (co *Controller) renderSigninError(c *gin.Context, message string) {
	c.Data(http.StatusBadRequest,
		"text/html; charset=utf-8",
		[]byte(`<html>
		<body style="background-color: #1e1e2e; color: #ffffff; text-align: center; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0;">
			<div>
				<h1>Could not sign in</h1>
				<p>`+template.HTMLEscapeString(message)+`</p>
				<p>You can close this tab and try again in Zed.</p>
			</div>
		</body>
		</html>`),
	)
}

// claimsFromRequest reads the access token out of the authorization header
// of a request and checks it.
//
// Zed sends the credentials as "<user id> <access token>".
func (co *Controller) claimsFromRequest(c *gin.Context) (*auth.Claims, bool) {
	if co.auth == nil {
		return nil, false
	}

	header := c.Request.Header.Get("Authorization")
	parts := strings.Split(header, " ")
	if len(parts) != 2 {
		return nil, false
	}

	headerId, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return nil, false
	}

	claims, err := co.auth.Verify(parts[1])
	if err != nil {
		logrus.Debugf("rejecting a request with an invalid access token: %v", err)
		return nil, false
	}
	if claims.UID != CanonicalUserID(headerId) {
		logrus.Debugf("rejecting a request whose token belongs to another user")
		return nil, false
	}
	return claims, true
}
