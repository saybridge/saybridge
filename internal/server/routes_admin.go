package server

import (
	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/app"
	"github.com/saybridge/saybridge/internal/authz"
	httphandler "github.com/saybridge/saybridge/internal/http"
	"github.com/saybridge/saybridge/internal/http/middleware"
	"github.com/saybridge/saybridge/internal/admin"
)

// RegisterAdminRoutes sets up admin, analytics, permissions, audit, and export endpoints.
func RegisterAdminRoutes(api *gin.RouterGroup, s *Server, c *app.Container, enforcer *authz.AuthzEnforcer) {
	// Admin (role-gated inside handlers)
	adminH := httphandler.NewAdminHandler(s.db)
	adminUsers := api.Group("/admin")
	{
		adminUsers.PATCH("/users/:id", adminH.UpdateUser)
		adminUsers.GET("/stats", adminH.GetStats)
	}

	// Admin Permissions (managed via Casbin)
	permH := httphandler.NewPermissionHandler(enforcer)
	adminPerms := api.Group("/admin/permissions")
	adminPerms.Use(middleware.RequirePermission(enforcer, s.db, "manage_permissions"))
	{
		adminPerms.GET("", permH.ListPolicies)
		adminPerms.POST("", permH.AddPolicy)
		adminPerms.DELETE("", permH.RemovePolicy)
		adminPerms.GET("/roles", permH.ListRoles)
		adminPerms.POST("/reload", permH.ReloadPolicy)
	}

	// Admin Analytics & Audit Logs
	analyticsSvc := admin.NewAnalyticsService(s.db)
	analyticsH := httphandler.NewAnalyticsHandler(analyticsSvc, c.AuditRepo)

	exportH := httphandler.NewExportHandler(s.db)

	admin := api.Group("/admin")
	{
		admin.GET("/analytics", analyticsH.GetDashboard)
		admin.GET("/audit", analyticsH.GetAuditLogs)
		admin.GET("/audit/export", analyticsH.ExportAuditLogs)
		admin.POST("/export/user/:id", exportH.ExportUser)
		admin.POST("/export/workspace", exportH.ExportWorkspace)
	}
}
