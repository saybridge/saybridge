package server

import (
	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/app"
)

// RegisterUserRoutes sets up user profile, search, presence, and file endpoints.
func RegisterUserRoutes(api *gin.RouterGroup, c *app.Container) {
	// Users
	users := api.Group("/users")
	{
		users.GET("/search", c.UserH.SearchUsers)
		users.GET("/me", c.UserH.GetProfile)
		users.PATCH("/me", c.UserH.UpdateProfile)
		users.POST("/me/avatar", c.UserH.UpdateAvatar)
		users.PATCH("/me/settings", c.UserH.UpdateSettings)
	}

	// Presence
	api.POST("/users/presence", c.PresenceH.UpdatePresence)

	// Files
	api.POST("/files/presign", c.FileH.PresignUpload)
	api.POST("/files/upload", c.FileH.UploadFile)
	api.POST("/files/:id/confirm", c.FileH.ConfirmUpload)
	api.GET("/files/download/:id", c.FileH.DownloadFile)
	api.DELETE("/files/:id", c.FileH.DeleteFile)
	api.GET("/files/my", c.FileH.ListUserFiles)
	api.GET("/files/shared", c.FileH.ListSharedFiles)
	api.GET("/files/all", c.FileH.ListAllFiles)
}
