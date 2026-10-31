package zed

import (
	"strings"

	"zedex/zed/auth"

	"github.com/gin-gonic/gin"
)

// Options configures the HTTP API.
type Options struct {
	// EnableExtensionStore serves the extension index from disk. When it is
	// off the requests are proxied to Zed.
	EnableExtensionStore bool

	// EnableLogin lets zedex handle the sign in requests.
	EnableLogin bool

	// EnableReleases serves the latest release from disk. When it is off the
	// requests are proxied to Zed.
	EnableReleases bool

	// EnableReleaseNotes serves the release notes from disk. When it is off
	// the requests are proxied to Zed.
	EnableReleaseNotes bool

	// Client talks to the Zed API.
	Client Client

	// Port is the port the server listens on.
	Port int

	// Auth authenticates the users. When it is nil every visitor gets an
	// anonymous identity.
	Auth *auth.Manager
}

type API struct {
	enableExtensionStore bool
	enableLogin          bool
	enableReleases       bool
	enableReleaseNotes   bool
	zedClient            Client
	port                 int
	// auth authenticates the users. When it is nil every visitor gets an
	// anonymous identity.
	auth *auth.Manager
	// rpc allows tests to inject a preconfigured RPC handler.
	rpc *RpcHandler
}

func NewAPI(opts Options) API {
	return API{
		zedClient:            opts.Client,
		port:                 opts.Port,
		enableExtensionStore: opts.EnableExtensionStore,
		enableLogin:          opts.EnableLogin,
		enableReleases:       opts.EnableReleases,
		enableReleaseNotes:   opts.EnableReleaseNotes,
		auth:                 opts.Auth,
	}
}

func (api *API) Router() *gin.Engine {
	router := gin.Default()
	controller := NewController(
		api.enableExtensionStore,
		api.enableLogin,
		api.enableReleases,
		api.enableReleaseNotes,
		api.zedClient,
		api.port,
	)
	if api.rpc != nil {
		controller.rpcHandler = *api.rpc
	}
	if api.auth != nil {
		controller.SetAuth(api.auth)
	}
	router.GET("/extensions", controller.Extensions)
	router.GET("/extensions/updates", controller.ExtensionUpdates)
	router.GET("/extensions/:id", controller.Extension)
	router.GET("/extensions/:id/download", controller.DownloadExtension)
	router.GET("/extensions/:id/:version/download", controller.DownloadExtension)

	router.GET("/releases/stable/latest/asset", controller.LatestVersion)

	router.GET("/api/*path", func(c *gin.Context) {
		if c.Request.URL.Path == "/api/releases/latest" && api.enableReleases {
			controller.LatestVersion(c)
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/api/release_notes/v2/stable/") && api.enableReleaseNotes {
			controller.LatestReleaseNotes(c)
			return
		}

		// Redirect to zed.host if not /api/releases
		c.Redirect(301, controller.zed.host+c.Request.URL.RequestURI())
	})
	router.GET("/native_app_signin", controller.NativeAppSignin)
	router.GET("/native_app_signin_succeeded", controller.NativeAppSigninSucceeded)
	router.GET("/oauth/start", controller.OAuthStart)
	router.GET("/oauth/callback", controller.OAuthCallback)
	router.GET("/oauth/guest", controller.OAuthGuest)
	router.GET("/rpc", controller.HandleRpcRequest)
	router.GET("/handle-rpc", controller.HandleWebSocketRequest)
	router.GET("/favicon.ico", func(c *gin.Context) {
		c.String(200, "plain/text", "")
	})

	router.POST("/client/llm_tokens", func(c *gin.Context) {
		c.JSON(200, NewLLMToken())
	})
	router.GET("/client/users/connect", controller.ConnectWebSocket)
	router.GET("/client/users/me", controller.AuthenticatedUser)

	router.GET("/account", controller.Account)
	router.GET("/avatar/:id", controller.Avatar)

	// LLM API endpoints. Zed calls these when the cloud language model
	// provider is enabled.
	router.GET("/models", controller.Models)
	router.GET("/edit_prediction_experiments", controller.EditPredictionExperiments)
	router.GET("/client/system_settings", controller.SystemSettings)
	router.PATCH("/client/system_settings", controller.UpdateSystemSettings)
	router.POST("/predict_edits/v2", controller.PredictEdits)
	router.POST("/predict_edits/v3", controller.PredictEdits)
	router.POST("/predict_edits/v4", controller.PredictEdits)
	router.POST("/predict_edits/raw", controller.PredictEdits)
	router.POST("/predict_edits/accept", controller.PredictEdits)
	router.POST("/predict_edits/reject", controller.PredictEdits)
	router.POST("/predict_edits/settled", controller.PredictEdits)
	router.POST("/web_search", controller.WebSearch)
	router.POST("/telemetry/events", controller.TelemetryEvents)
	router.POST("/client/feedback/edit_prediction", controller.Feedback)
	router.POST("/client/feedback/agent_thread", controller.Feedback)
	router.POST("/client/feedback/agent_thread_comments", controller.Feedback)
	return router
}
