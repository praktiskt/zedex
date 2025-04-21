package zed

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"zedex/llm"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

type API struct {
	localMode bool
	zedClient Client
}

func NewAPI(localMode bool, zedClient Client) API {
	return API{
		zedClient: zedClient,
		localMode: localMode,
	}
}

func (api *API) Router() *gin.Engine {
	router := gin.Default()
	controller := NewController(api.localMode, api.zedClient)
	router.GET("/extensions", controller.Extensions)
	router.GET("/extensions/:id/download", controller.DownloadExtension)
	router.GET("/extensions/:id/:version/download", controller.DownloadExtension)

	router.GET("/api/*path", func(c *gin.Context) {
		if c.Request.URL.Path == "/api/releases/latest" && api.localMode {
			controller.LatestVersion(c)
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/release_notes/v2/stable/") && api.localMode {
			controller.LatestReleaseNotes(c)
			return
		}

		// Redirect to zed.host if not /api/releases
		c.Redirect(301, controller.zed.host+c.Request.URL.RequestURI())
	})
	router.GET("/native_app_signin", controller.NativeAppSignin)
	router.GET("/native_app_signin_succeeded", controller.NativeAppSigninSucceeded)
	router.GET("/rpc", controller.HandleRpcRequest)
	router.GET("/handle-rpc", controller.HandleWebSocketRequest)
	router.GET("/favicon.ico", func(c *gin.Context) {
		c.String(200, "plain/text", "")
	})

	router.POST("/predict_edits/v2", func(c *gin.Context) {
		autoComplete := struct {
			RequestId     string `json:"request_id"`
			OutputExcerpt string `json:"output_excerpt"`
		}{
			RequestId: uuid.New().String(),
		}

		if _, ok := os.LookupEnv("OPENAI_COMPATIBLE_DISABLE"); ok {
			c.JSON(200, autoComplete)
		}

		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logrus.Error(err)
			return
		}

		incoming := struct {
			SpeculatedOutput string `json:"speculated_output"`
		}{}
		if err := json.Unmarshal(b, &incoming); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if _, ok := os.LookupEnv("OPENAI_COMPATIBLE_SYSTEM_PROMPT"); !ok {
			// TODO: remove this garbage.
			os.Setenv("OPENAI_COMPATIBLE_SYSTEM_PROMPT", `You are an autocomplete engine.

RULES:
* Only respond with code (nothing else).
* Never include code blocks (triple backticks) in your response.
* YOU MUST INCLUDE ALL PLACEHOLDERS "<|editable_region_start|>" AND "<|editable_region_end|>" IN YOUR RESPONSE.
* YOU MAY ALTER ALL CODE CONTAINED WITHIN "<|editable_region_start|>" AND "<|editable_region_end|>".
* Always respond with the complete input, but also auto-complete with code AFTER the placeholder <|user_cursor_is_here|>
	* If autocompleting a class/struct, only complete that class/struct - do not create new classes/structs.
	* If autocompleting a function, only complete that function - do not create new functions.
	* If autocompleting a variable, only complete that variable - do not create new variables.`)
		}

		resp, err := llm.GetOpenAICompatibleResponse(incoming.SpeculatedOutput)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		autoComplete.OutputExcerpt = `<|start_of_file|>` + resp.GetLastResponse()
		c.JSON(200, autoComplete)
	})

	return router
}
