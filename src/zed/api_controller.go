package zed

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"

	"zedex/llm"
	"zedex/utils"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type Controller struct {
	zed                  Client
	llm                  *llm.OpenAIHost
	port                 int
	enableExtensionStore bool
	enableLogin          bool
	enableEditPrediction bool
	enableReleases       bool
	enableReleaseNotes   bool

	editPredictClient EditPredictClient
}

func NewController(
	enableExtensionStore bool,
	enableLogin bool,
	enableEditPrediction bool,
	enableReleases bool,
	enableReleaseNotes bool,
	zedClient Client,
	port int,
) Controller {
	_, envExists := os.LookupEnv("OPENAI_COMPATIBLE_API_KEY")
	oai := llm.NewOpenAIHost(
		utils.EnvWithFallback("OPENAI_COMPATIBLE_HOST", "https://api.groq.com/openai/v1/chat/completions"),
		utils.IfElse(envExists, "OPENAI_COMPATIBLE_API_KEY", "GROQ_API_KEY"),
	).
		WithModel(utils.EnvWithFallback("OPENAI_COMPATIBLE_MODEL", "meta-llama/llama-4-scout-17b-16e-instruct")).
		WithTemperature(0.1). // TODO: Use env.
		WithSystemPrompt(utils.EnvWithFallback("OPENAI_COMPATIBLE_SYSTEM_PROMPT", `You are a code autocomplete engine.

RULES:
* Only respond with code (nothing else).
* YOU MUST INCLUDE ALL PLACEHOLDERS "<|editable_region_start|>" AND "<|editable_region_end|>" IN YOUR RESPONSE.
* YOU MAY ALTER ALL CODE CONTAINED WITHIN "<|editable_region_start|>" AND "<|editable_region_end|>".
* ALWAYS AUTO COMPLETE AS LITTLE AS POSSIBLE`))
	return Controller{
		zed:                  zedClient,
		enableExtensionStore: enableExtensionStore,
		enableLogin:          enableLogin,
		enableEditPrediction: enableEditPrediction,
		enableReleases:       enableReleases,
		enableReleaseNotes:   enableReleaseNotes,
		port:                 port,
		editPredictClient:    NewEditPredictClient(*oai),
	}
}

func (co *Controller) Extensions(c *gin.Context) {
	var extensions Extensions
	var err error

	if co.enableExtensionStore {
		extensionsFile := path.Join(co.zed.extensionsLocalDir, "extensions.json")
		extensions, err = co.zed.LoadExtensionIndex(extensionsFile)
	} else {
		extensions, err = co.zed.GetExtensionsIndex()
	}

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
	host := fmt.Sprintf("http://127.0.0.1:%s/native_app_signin?user_id=1&access_token=%s", portStr, enc)
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

func (co *Controller) HandleRpcRequest(c *gin.Context) {
	baseURL := utils.EnvWithFallback("BASE_URL", fmt.Sprintf("http://127.0.0.1:%v", co.port))
	location := fmt.Sprintf("%s/handle-rpc", baseURL)
	c.Redirect(301, location)
}

// https://github.com/zed-industries/zed/blob/1e22faebc9f9c8da685a34b15c17f2bc2b418b26/crates/collab/src/rpc.rs#L1092
func (co *Controller) HandleWebSocketRequest(c *gin.Context) {
	rpc := RpcHandler{}
	rpc.HandleRequest(c)
}

func (co *Controller) HandleEditPredictRequest(c *gin.Context) {
	epr := EditPredictRequest{}
	if err := c.ShouldBindJSON(&epr); err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	resp, err := co.editPredictClient.HandleRequest(epr)
	if err != nil {
		logrus.Error(err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, resp)
}
