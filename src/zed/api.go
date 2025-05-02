package zed

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type API struct {
	enableExtensionStore bool
	enableLogin          bool
	enableEditPrediction bool
	enableReleases       bool
	enableReleaseNotes   bool
	zedClient            Client
	port                 int
}

func NewAPI(
	enableExtensionStore bool,
	enableLogin bool,
	enableEditPrediction bool,
	enableReleases bool,
	enableReleaseNotes bool,
	zedClient Client,
	port int,
) API {
	return API{
		zedClient:            zedClient,
		port:                 port,
		enableExtensionStore: enableExtensionStore,
		enableLogin:          enableLogin,
		enableEditPrediction: enableEditPrediction,
		enableReleases:       enableReleases,
		enableReleaseNotes:   enableReleaseNotes,
	}
}

func (api *API) Router() *gin.Engine {
	router := gin.Default()
	controller := NewController(
		api.enableExtensionStore,
		api.enableLogin,
		api.enableEditPrediction,
		api.enableReleases,
		api.enableReleaseNotes,
		api.zedClient,
		api.port,
	)
	router.GET("/extensions", controller.Extensions)
	router.GET("/extensions/:id/download", controller.DownloadExtension)
	router.GET("/extensions/:id/:version/download", controller.DownloadExtension)

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
	router.GET("/rpc", controller.HandleRpcRequest)
	router.GET("/handle-rpc", controller.HandleWebSocketRequest)
	router.GET("/favicon.ico", func(c *gin.Context) {
		c.String(200, "plain/text", "")
	})

	router.POST("/predict_edits/v2", controller.HandleEditPredictRequest)
	return router
}
