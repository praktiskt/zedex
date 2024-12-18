package zed

import "github.com/gin-gonic/gin"

type API struct{}

func NewAPI() API {
	return API{}
}

func (api *API) Router(localMode bool) *gin.Engine {
	router := gin.Default()
	controller := NewController(localMode)
	router.GET("/extensions", controller.Extensions)
	router.GET("/extensions/:id/download", controller.DownloadExtension)

	// TODO: Passthrough for now. Should we do something else?
	router.GET("/api/*path", func(c *gin.Context) {
		c.Redirect(301, controller.zed.host+c.Request.URL.RequestURI())
	})

	return router
}
