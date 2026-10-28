package zed

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math"
	mrand "math/rand"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"zedex/utils"

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

	rpcHandler        RpcHandler
}

func NewController(
	enableExtensionStore bool,
	enableLogin bool,
	enableReleases bool,
	enableReleaseNotes bool,
	zedClient Client,
	port int,
) Controller {
	return Controller{
		zed:                  zedClient,
		enableExtensionStore: enableExtensionStore,
		enableLogin:          enableLogin,
		enableReleases:       enableReleases,
		enableReleaseNotes:   enableReleaseNotes,
		port:                 port,
		rpcHandler:           NewRpcHandler(),
	}
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

	// TODO: Do we care about version?
	// minSchemaVersion := c.DefaultQuery("min_schema_version", "0")
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

func (co *Controller) NativeAppSignin(c *gin.Context) {
	portStr := c.Query("native_app_port")
	pubKey := c.Query("native_app_public_key")

	enc, err := encryptStringV1(pubKey, "a")
	if err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	// user_id must be numeric, possibly a reference to github id
	// https://api.github.com/users/<user>
	uid := mrand.Intn(math.MaxInt)
	host := fmt.Sprintf("http://127.0.0.1:%s/native_app_signin?user_id=%v&access_token=%s", portStr, uid, enc)
	c.Redirect(302, host)
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
	c.Data(200,
		"text/html; charset=utf-8",
		[]byte(`<html>
		<body style="background-color: #1e1e2e; color: #ffffff; text-align: center; display: flex; justify-content: center; align-items: center">
			<p>You are logged in anonymously with Zedex.</p>
		</body>
		</html>`,
		),
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
