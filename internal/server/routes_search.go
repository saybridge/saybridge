package server

import (
	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/app"
)

// RegisterSearchRoutes sets up search endpoints for messages, users, and rooms.
func RegisterSearchRoutes(api *gin.RouterGroup, c *app.Container) {
	search := api.Group("/search")
	{
		search.GET("/messages", c.SearchH.SearchMessages)
		search.GET("/users", c.SearchH.SearchUsers)
		search.GET("/rooms", c.SearchH.SearchRooms)
	}
}
