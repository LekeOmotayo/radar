package controllers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lekeomotayo/radar/internal/api/services"
)

// ToolController handles HTTP routing for runtime diagnostics and tool information.
type ToolController struct {
	service services.ToolService
}

// NewToolController creates an initialized ToolController.
func NewToolController(service services.ToolService) *ToolController {
	return &ToolController{
		service: service,
	}
}

// GetMetrics handles GET /api/metrics.
func (ctl *ToolController) GetMetrics(c *gin.Context) {
	metrics := ctl.service.GetRuntimeMetrics(c.Request.Context())
	c.JSON(http.StatusOK, metrics)
}

// GetCatalog handles GET /api/tools.
func (ctl *ToolController) GetCatalog(c *gin.Context) {
	tools := ctl.service.GetToolCatalog(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"tools": tools,
	})
}

// HealthCheck handles GET /api/health.
func (ctl *ToolController) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "radar",
	})
}
