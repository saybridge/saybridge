package server

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/app"
	"github.com/saybridge/saybridge/internal/authz"
	httphandler "github.com/saybridge/saybridge/internal/http"
	"github.com/saybridge/saybridge/internal/http/middleware"
)

// RegisterRoomRoutes sets up room CRUD, member management, ban, prune, export, and file endpoints.
func RegisterRoomRoutes(api *gin.RouterGroup, s *Server, c *app.Container, enforcer *authz.AuthzEnforcer) {
	rooms := api.Group("/rooms")
	{
		rooms.POST("", c.RoomH.CreateRoom)
		rooms.GET("", c.RoomH.ListRooms)
		rooms.GET("/slug/:slug", c.RoomH.GetRoomBySlug)
		rooms.GET("/:id", c.RoomH.GetRoomDetails)
		rooms.POST("/:id/members", c.RoomH.InviteMember)
		rooms.DELETE("/:id/members/:userId", middleware.RequirePermission(enforcer, s.db, "kick_member"), c.RoomH.KickMember)
		rooms.POST("/:id/leave", c.RoomH.LeaveRoom)
		rooms.POST("/:id/read", c.ReadPosH.MarkAsRead)
		rooms.PUT("/:id/e2ee", middleware.RequirePermission(enforcer, s.db, "toggle_e2ee"), c.RoomH.ToggleE2EE)
		rooms.GET("/:id/actions", c.RoomH.GetActions)

		// Room member settings (favorite, mute)
		roomMemberH := httphandler.NewRoomMemberHandler(s.db)
		rooms.POST("/:id/favorite", roomMemberH.ToggleFavorite)
		rooms.POST("/:id/mute", roomMemberH.ToggleMute)

		// Ban management
		banH := httphandler.NewBanHandler(s.db)
		rooms.POST("/:id/ban/:userId", middleware.RequirePermission(enforcer, s.db, "ban_user"), banH.BanUser)
		rooms.POST("/:id/unban/:userId", middleware.RequirePermission(enforcer, s.db, "ban_user"), banH.UnbanUser)
		rooms.GET("/:id/banned", banH.GetBannedUsers)

		// Prune, export, files
		rooms.POST("/:id/prune", middleware.RequirePermission(enforcer, s.db, "prune_messages"), httphandler.NewPruneHandler(s.db).PruneMessages)
		rooms.GET("/:id/export", middleware.RequirePermission(enforcer, s.db, "export_messages"), httphandler.NewMessageExportHandler(s.db).ExportMessages)
		rooms.GET("/:id/files", c.FileH.ListRoomFiles)
	}
}

// initEnforcer initializes the Casbin authorization enforcer.
func initEnforcer() *authz.AuthzEnforcer {
	modelPath := "internal/authz/model.conf"
	policyPath := "internal/authz/default_policy.csv"
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		modelPath = "backend/internal/authz/model.conf"
		policyPath = "backend/internal/authz/default_policy.csv"
	}
	enforcer, err := authz.NewEnforcer(modelPath, policyPath)
	if err != nil {
		log.Fatalf("Failed to initialize Casbin enforcer: %v", err)
	}
	return enforcer
}
