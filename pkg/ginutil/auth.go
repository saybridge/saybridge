package ginutil

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AuthInfo holds the extracted authentication information from a Gin context.
type AuthInfo struct {
	UserID   string
	TenantID string
	Role     string
}

// MustGetAuth extracts user_id, tenant_id, and role from gin.Context.
// If user is not authenticated, it sends a 401 response and returns ok=false.
func MustGetAuth(c *gin.Context) (auth AuthInfo, ok bool) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return AuthInfo{}, false
	}
	auth.UserID = userIDVal.(string)
	auth.TenantID = c.GetString("tenant_id")
	auth.Role = c.GetString("role")
	return auth, true
}

// MustGetAdmin is MustGetAuth + admin role check.
// Sends 403 if user is not an admin.
func MustGetAdmin(c *gin.Context) (auth AuthInfo, ok bool) {
	auth, ok = MustGetAuth(c)
	if !ok {
		return
	}
	if auth.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		return auth, false
	}
	return auth, true
}
