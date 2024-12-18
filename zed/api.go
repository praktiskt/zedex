package zed

import "github.com/gin-gonic/gin"

type API struct{}

func NewAPI() API {
	return API{}
}

func (api *API) Router() *gin.Engine {
	router := gin.Default()
	controller := NewController()
	router.GET("/extensions", controller.Extensions)
	// /extensions/scls/download?min_schema_version=0&max_schema_version=1&min_wasm_api_version=0.0.1&max_wasm_api_version=0.2.0
	router.GET("/extensions/:id/download", controller.DownloadExtension)

	return router
}
