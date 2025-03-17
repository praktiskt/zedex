package zed

import (
	"strings"

	"github.com/gin-gonic/gin"
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

	return router
}
