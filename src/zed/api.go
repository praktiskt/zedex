package zed

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type API struct {
	localMode            bool
	hijackExtensionStore bool
	hijackLogin          bool
	hijackEditPrediction bool
	hijackReleases       bool
	hijackReleaseNotes   bool
	zedClient            Client
	port                 int
}

func NewAPI(localMode bool, zedClient Client, port int) API {
	return API{
		zedClient:            zedClient,
		localMode:            localMode,
		port:                 port,
		hijackExtensionStore: false,
		hijackLogin:          false,
		hijackEditPrediction: false,
		hijackReleases:       false,
		hijackReleaseNotes:   false,
	}
}

func (api *API) Router() *gin.Engine {
	router := gin.Default()
	controller := NewController(api.localMode, api.zedClient, api.port)
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

	router.POST("/predict_edits/v2", controller.HandleEditPredictRequest)

	return router
}
