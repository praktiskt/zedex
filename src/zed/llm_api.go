package zed

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// The endpoints in this file are the "LLM API" endpoints that Zed's cloud
// provider talks to. Zed only calls them when the cloud language model
// provider is enabled; zedex serves them so that the client does not log
// errors while it starts up.

// Models returns the list of models that Zed's cloud provider should show.
func (co *Controller) Models(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"models":             []any{},
		"default_model":      nil,
		"default_fast_model": nil,
		"recommended_models": []any{},
	})
}

// EditPredictionExperiments returns the list of active edit prediction
// experiments. zedex does not run experiments, so the list is empty.
func (co *Controller) EditPredictionExperiments(c *gin.Context) {
	c.JSON(http.StatusOK, []string{})
}

// SystemSettings returns the settings that Zed syncs with the server.
func (co *Controller) SystemSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"selected_organization_id": nil,
	})
}

// UpdateSystemSettings stores the settings that Zed syncs with the server.
func (co *Controller) UpdateSystemSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"selected_organization_id": nil,
	})
}

// PredictEdits handles Zed's edit prediction requests. zedex does not
// implement edit prediction itself: Zed can be configured with a custom
// provider instead. Returning an empty response keeps the client from
// spamming errors.
func (co *Controller) PredictEdits(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "edit prediction is not served by zedex; configure a custom provider in Zed",
	})
}

// WebSearch handles Zed's web search requests.
func (co *Controller) WebSearch(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": "web search is not served by zedex",
	})
}

// TelemetryEvents accepts (and discards) telemetry events.
func (co *Controller) TelemetryEvents(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Feedback accepts feedback submissions.
func (co *Controller) Feedback(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
