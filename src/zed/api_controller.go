package zed

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"zedex/utils"
	"zedex/zed/auth"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Controller struct {
	zed                  Client
	port                 int
	enableExtensionStore bool
	enableLogin          bool
	enableReleases       bool
	enableReleaseNotes   bool

	// auth authenticates the users. When it is nil every visitor gets an
	// anonymous identity.
	auth *auth.Manager

	// authFlow keeps track of the sign ins that are in progress.
	authFlow *auth.Flow

	rpcHandler RpcHandler
}

func NewController(
	enableExtensionStore bool,
	enableLogin bool,
	enableReleases bool,
	enableReleaseNotes bool,
	zedClient Client,
	port int,
) Controller {
	rpc := NewRpcHandler()
	rpc.port = port

	return Controller{
		zed:                  zedClient,
		rpcHandler:           rpc,
		enableExtensionStore: enableExtensionStore,
		enableLogin:          enableLogin,
		enableReleases:       enableReleases,
		enableReleaseNotes:   enableReleaseNotes,
		port:                 port,
		authFlow:             auth.NewFlow(),
	}
}

// SetAuth turns on the authentication of users.
func (co *Controller) SetAuth(manager *auth.Manager) {
	co.auth = manager
	co.rpcHandler.SetAuth(manager)
}

func (co *Controller) Extensions(c *gin.Context) {
	if !co.enableExtensionStore {
		u := co.zed.apiHost + "/extensions"
		if c.Request.URL.RawQuery != "" {
			u += "?" + c.Request.URL.RawQuery
		}
		resp, err := http.Get(u)
		if err != nil {
			logrus.Error(err)
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}
	var extensions Extensions
	var err error

	extensionsFile := path.Join(co.zed.extensionsLocalDir, "extensions.json")
	extensions, err = co.zed.LoadExtensionIndex(extensionsFile)

	if err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	maxSchemaVersion := c.DefaultQuery("max_schema_version", "100")
	maxSchemaVersionInt, err := strconv.Atoi(maxSchemaVersion)
	if err != nil {
		logrus.Error(err)
		c.JSON(400, gin.H{
			"error":   "Bad Request",
			"message": "max_schema_version must be an integer",
		})
		return
	}

	extensions = extensions.Filter(func(e Extension) bool {
		return e.SchemaVersion <= maxSchemaVersionInt
	})

	if filter := c.DefaultQuery("filter", ""); filter != "" {
		extensions = extensions.Filter(func(e Extension) bool {
			return strings.Contains(strings.ToLower(e.AsJsonStr()), strings.ToLower(filter))
		})
	}

	if filter := c.DefaultQuery("provides", ""); filter != "" {
		extensions = extensions.FilterByProvides(filter)
	}

	c.JSON(200, extensions.AsWrapped())
}

func (co *Controller) DownloadExtension(c *gin.Context) {
	if !co.enableExtensionStore {
		u := co.zed.apiHost + c.Request.URL.Path
		if c.Request.URL.RawQuery != "" {
			u += "?" + c.Request.URL.RawQuery
		}
		resp, err := http.Get(u)
		if err != nil {
			logrus.Error(err)
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		c.Data(resp.StatusCode, contentType, body)
		return
	}
	id := c.Param("id")

	// TODO: Do we care about version?	// minSchemaVersion := c.DefaultQuery("min_schema_version", "0")
	// minSchemaVersionInt, err := strconv.Atoi(minSchemaVersion)
	// if err != nil {
	// 	c.JSON(400, gin.H{
	// 		"error":   "Bad Request",
	// 		"message": "min_schema_version must be an integer",
	// 	})
	// 	return
	// }
	// maxSchemaVersion := c.Query("max_schema_version")
	// minWasmApiVersion := c.Query("min_wasm_api_version")
	// maxWasmApiVersion := c.Query("max_wasm_api_version")

	extension := Extension{ID: id}
	var bytes []byte
	var err error

	if co.enableExtensionStore {
		bytes, err = co.zed.LoadExtensionArchive(extension)
	} else {
		bytes, err = co.zed.DownloadExtensionArchiveDefault(extension)
	}

	if err != nil {
		if co.enableExtensionStore && os.IsNotExist(err) {
			// The extension has not been downloaded into the local cache.
			c.JSON(404, gin.H{
				"error":   "Not Found",
				"message": fmt.Sprintf("extension %q is not in the local cache", id),
			})
			return
		}
		logrus.Error(err)
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.Data(200, "application/octet-stream", bytes)
}

func (co *Controller) LatestVersion(c *gin.Context) {
	var v Version
	var err error
	if co.enableReleases {
		versionFile := path.Join(co.zed.extensionsLocalDir, "latest_release.json")
		v, err = co.zed.LoadLatestZedVersionFromFile(versionFile)
	} else {
		v, err = co.zed.GetLatestZedVersion()
	}

	if err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(200, v)
}

func (co *Controller) LatestReleaseNotes(c *gin.Context) {
	var v ReleaseNotes
	var err error
	if co.enableReleaseNotes {
		versionFile := path.Join(co.zed.extensionsLocalDir, "latest_release_notes.json")
		v, err = co.zed.LoadReleaseNotes(versionFile)
	} else {
		v, err = co.zed.GetLatestReleaseNotes()
	}

	if err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(200, v)
}

// v1 is a reference to rusts rsa crate
func encryptStringV1(base64PublicKey, plaintext string) (string, error) {
	pubKeyBytes, err := base64.URLEncoding.DecodeString(base64PublicKey)
	if err != nil {
		return "", err
	}

	rsaPubKey, err := x509.ParsePKCS1PublicKey(pubKeyBytes)
	if err != nil {
		return "", err
	}

	encryptedBytes, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPubKey, []byte(plaintext), nil)
	if err != nil {
		return "", err
	}

	encryptedBase64 := base64.URLEncoding.EncodeToString(encryptedBytes)
	return encryptedBase64, nil
}

func randomToken() (string, error) {
	tokenBytes := make([]byte, 48)
	_, err := rand.Read(tokenBytes)
	if err != nil {
		return "", err
	}

	// Use base64.URLEncoding to get URL-safe encoding
	encodedToken := base64.URLEncoding.EncodeToString(tokenBytes)
	return encodedToken, nil
}

func (co *Controller) NativeAppSigninSucceeded(c *gin.Context) {
	c.Data(200,
		"text/html; charset=utf-8",
		[]byte(`<html>
		<body style="background-color: #1e1e2e; color: #ffffff; text-align: center; display: flex; justify-content: center; align-items: center">
			<p>You should now be signed into Zed. You can close this tab.</p>
		</body>
		</html>`,
		),
	)
}

func (co *Controller) Account(c *gin.Context) {
	heading := "Signed in to Zedex"
	description := "You can close this tab and return to Zed."
	if co.auth == nil {
		heading = "Signed in anonymously to Zedex"
		description = "No login required. You can close this tab and return to Zed."
	}

	c.Data(200,
		"text/html; charset=utf-8",
		[]byte(`<html>
		<body style="background-color: #1e1e2e; color: #ffffff; text-align: center; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0;">
			<div>
				<h1>`+heading+`</h1>
				<p>`+description+`</p>
			</div>
		</body>
		</html>`),
	)
}

func (co *Controller) HandleRpcRequest(c *gin.Context) {
	baseURL := utils.EnvWithFallback("BASE_URL", fmt.Sprintf("http://127.0.0.1:%v", co.port))
	location := fmt.Sprintf("%s/handle-rpc", baseURL)
	c.Redirect(301, location)
}

// https://github.com/zed-industries/zed/blob/1e22faebc9f9c8da685a34b15c17f2bc2b418b26/crates/collab/src/rpc.rs#L1092
func (co *Controller) HandleWebSocketRequest(c *gin.Context) {
	co.rpcHandler.HandleRequest(c)
}

func (co *Controller) ExtensionUpdates(c *gin.Context) {
	if !co.enableExtensionStore {
		u := co.zed.apiHost + "/extensions/updates"
		if c.Request.URL.RawQuery != "" {
			u += "?" + c.Request.URL.RawQuery
		}
		resp, err := http.Get(u)
		if err != nil {
			logrus.Error(err)
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}
	c.JSON(200, wrappedExtensions{Data: Extensions{}})
}

func (co *Controller) Extension(c *gin.Context) {
	id := c.Param("id")
	if !co.enableExtensionStore {
		u := co.zed.apiHost + "/extensions/" + id
		if c.Request.URL.RawQuery != "" {
			u += "?" + c.Request.URL.RawQuery
		}
		resp, err := http.Get(u)
		if err != nil {
			logrus.Error(err)
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		c.Data(resp.StatusCode, "application/json", body)
		return
	}

	extensionsFile := path.Join(co.zed.extensionsLocalDir, "extensions.json")
	extensions, err := co.zed.LoadExtensionIndex(extensionsFile)
	if err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var filtered Extensions
	for _, e := range extensions {
		if e.ID == id {
			filtered = append(filtered, e)
		}
	}
	c.JSON(200, filtered.AsWrapped())
}

func (co *Controller) ConnectWebSocket(c *gin.Context) {
	if co.auth != nil {
		if _, ok := co.claimsFromRequest(c); !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Unauthorized",
				"message": "the access token is missing or invalid",
			})
			return
		}
	}

	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logrus.Error(err)
		return
	}
	defer conn.Close()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (co *Controller) AuthenticatedUser(c *gin.Context) {
	if co.auth != nil {
		claims, ok := co.claimsFromRequest(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":   "Unauthorized",
				"message": "the access token is missing or invalid",
			})
			return
		}

		uid := claims.UID
		username := co.auth.Username(claims)
		resp := NewGetAuthenticatedUsersResponseFor(UserIdentity{
			LegacyUserID: int(uid),
			Username:     username,
			Name:         co.auth.Name(claims),
			GithubLogin:  co.auth.GithubLogin(claims),
			AvatarURL:    co.avatarURLFor(c, uid, co.auth.AvatarURL(claims)),
		})
		c.JSON(200, resp)
		return
	}

	auth := c.Request.Header.Get("Authorization")
	parts := strings.Split(auth, " ")

	var legacyUserId int64 = 1
	if len(parts) > 0 && parts[0] != "" {
		if uid, err := strconv.ParseUint(parts[0], 10, 64); err == nil {
			// Zed truncates credentials to u32 for the cloud API and models
			// legacy_user_id as i32, so canonicalize the id the same way here.
			legacyUserId = int64(CanonicalUserID(uid))
		}
	}

	name, _, _ := identityFor(uint64(legacyUserId))
	resp := NewGetAuthenticatedUsersResponseFor(UserIdentity{
		LegacyUserID: int(legacyUserId),
		Username:     name,
		AvatarURL:    co.avatarURL(c, uint64(legacyUserId)),
	})
	c.JSON(200, resp)
}

// avatarURL returns the URL of the avatar zedex serves for a user.
func (co *Controller) avatarURL(c *gin.Context, userId uint64) string {
	base := utils.EnvWithFallback("BASE_URL", fmt.Sprintf("http://127.0.0.1:%v", co.port))
	if host := c.Request.Host; host != "" && !strings.HasPrefix(base, "http://127.0.0.1") {
		base = "http://" + host
	}
	return fmt.Sprintf("%s/avatar/%d", base, userId)
}

// avatarURLFor returns the picture to show for a user. It is the picture the
// provider reports, or the identicon zedex generates when there is none.
func (co *Controller) avatarURLFor(c *gin.Context, userId uint64, providerAvatar string) string {
	if providerAvatar != "" {
		return providerAvatar
	}
	return co.avatarURL(c, userId)
}
