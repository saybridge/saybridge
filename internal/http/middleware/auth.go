package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/pkg/crypto"
	"github.com/saybridge/saybridge/pkg/response"
	"github.com/redis/go-redis/v9"
)

// AuthMiddleware extracts and validates the JWT Access Token from the Authorization header,
// and checks if the session has been revoked in Redis.
func AuthMiddleware(jwtMgr *crypto.JWTManager, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read token from Authorization header or token query parameter
		tokenStr := ""
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			// Validate Bearer format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenStr = parts[1]
			}
		}

		if tokenStr == "" {
			tokenStr = c.Query("token")
		}

		if tokenStr == "" {
			response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Access token is required")
			return
		}

		// Verify RS256 signature and extract Claims
		claims, err := jwtMgr.VerifyAccessToken(tokenStr)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired access token: "+err.Error())
			return
		}

		// Check if the session has been blacklisted/revoked
		revokedKey := fmt.Sprintf("revoked_session:%s:%s", claims.Subject, claims.DeviceID)
		exists, err := rdb.Exists(c.Request.Context(), revokedKey).Result()
		if err == nil && exists > 0 {
			response.Error(c, http.StatusUnauthorized, "REVOKED_SESSION", "This session has been revoked or logged out")
			return
		}

		// Inject identity claims into Gin Context for subsequent API handlers to consume
		c.Set("user_id", claims.Subject)
		c.Set("tenant_id", claims.TenantID)
		c.Set("role", claims.Role)
		c.Set("device_id", claims.DeviceID)

		c.Next()
	}
}
