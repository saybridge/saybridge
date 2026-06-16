package server

import (
	"github.com/gin-gonic/gin"
	httphandler "github.com/saybridge/saybridge/internal/http"
	"gorm.io/gorm"
)

// RegisterSystemRoutes sets up public system endpoints (health, readiness, setup).
func RegisterSystemRoutes(r *gin.Engine, db *gorm.DB) {
	setupH := httphandler.NewSetupHandler(db)
	system := r.Group("/api/v1/system")
	{
		system.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true, "data": gin.H{"status": "healthy"}})
		})
		system.GET("/ready", func(c *gin.Context) {
			c.JSON(200, gin.H{"success": true, "data": gin.H{"status": "ready"}})
		})
		system.GET("/setup-check", setupH.CheckSetup)
		system.POST("/setup", setupH.CompleteSetup)
	}
}
