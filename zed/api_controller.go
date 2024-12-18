package zed

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

type Controller struct {
	zed Client
}

func NewController() Controller {
	return Controller{
		zed: NewZedClient(1),
	}
}

func (co *Controller) Extensions(c *gin.Context) {
	extensions, err := co.zed.GetExtensionsIndex()
	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	maxSchemaVersion := c.DefaultQuery("max_schema_version", "100")
	maxSchemaVersionInt, err := strconv.Atoi(maxSchemaVersion)
	if err != nil {
		c.JSON(400, gin.H{
			"error":   "Bad Request",
			"message": "max_schema_version must be an integer",
		})
		return
	}

	extensions = extensions.Filter(func(e Extension) bool {
		return e.SchemaVersion <= maxSchemaVersionInt
	})

	c.JSON(200, struct {
		Data Extensions `json:"data"`
	}{
		Data: extensions,
	})
}

func (co *Controller) DownloadExtension(c *gin.Context) {
	id := c.Param("id")
	minSchemaVersion := c.DefaultQuery("min_schema_version", "0")
	minSchemaVersionInt, err := strconv.Atoi(minSchemaVersion)
	if err != nil {
		c.JSON(400, gin.H{
			"error":   "Bad Request",
			"message": "min_schema_version must be an integer",
		})
		return
	}
	// maxSchemaVersion := c.Query("max_schema_version") // TODO: we don't need this
	minWasmApiVersion := c.Query("min_wasm_api_version")
	maxWasmApiVersion := c.Query("max_wasm_api_version")

	bytes, err := co.zed.DownloadExtensionArchive(
		Extension{ID: id},
		minSchemaVersionInt,
		minWasmApiVersion,
		maxWasmApiVersion,
	)
	if err != nil {
		c.JSON(500, gin.H{
			"error":   "Internal Server Error",
			"message": err.Error(),
		})
		return
	}

	c.Data(200, "application/octet-stream", bytes)
}
