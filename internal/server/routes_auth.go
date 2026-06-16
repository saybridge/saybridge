package server

import (
	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
	"github.com/saybridge/saybridge/internal/app"
	"github.com/saybridge/saybridge/internal/http/middleware"
)

// RegisterAuthRoutes sets up public authentication endpoints (register, login, refresh, logout).
func RegisterAuthRoutes(r *gin.Engine, c *app.Container, rdb *goredis.Client) {
	auth := r.Group("/api/v1/auth")
	auth.Use(middleware.DynamicRateLimitMiddleware(rdb))
	{
		auth.POST("/register", c.AuthH.Register)
		auth.POST("/login", c.AuthH.Login)
		auth.POST("/refresh", c.AuthH.Refresh)
		auth.POST("/logout", c.AuthH.Logout)
	}
}
