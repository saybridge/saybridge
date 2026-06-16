package server

import (
	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/app"
)

// RegisterMessageRoutes sets up message history and thread reply endpoints.
func RegisterMessageRoutes(api *gin.RouterGroup, c *app.Container) {
	messages := api.Group("/messages")
	{
		messages.GET("/room/:roomId", c.MessageH.GetMessageHistory)
		messages.GET("/thread/:parentId/replies", c.MessageH.GetThreadReplies)
	}
}
