package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/authz"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
	"gorm.io/gorm"
)

// RequirePermission is a Gin middleware that checks if the authenticated user
// has the specified permission/action on the requested resource.
func RequirePermission(enforcer *authz.AuthzEnforcer, db *gorm.DB, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		sysRole := c.GetString("role")

		// 1. Build Object from request context
		obj := authz.Object{
			Type: "system", // default for non-room routes
		}

		roomID := c.Param("id")
		if roomID == "" {
			roomID = c.Param("roomId")
		}

		if roomID != "" {
			var room domain.Room
			if err := db.First(&room, "id = ?", roomID).Error; err == nil {
				obj.Type = "room"
				obj.ID = room.ID
				if room.CreatedBy != nil {
					obj.OwnerID = *room.CreatedBy
				}
				obj.RoomType = room.Type
				obj.IsReadOnly = room.IsReadOnly
			}
		}

		// 2. Build Subject from JWT claims and room membership
		sub := authz.Subject{
			ID:       userID,
			Role:     sysRole,
			IsActive: true,
		}

		if obj.Type == "room" && roomID != "" {
			var member domain.RoomMember
			if err := db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&member).Error; err == nil {
				if member.IsBanned {
					sub.RoomRole = "banned"
					sub.Role = "banned"
				} else {
					sub.RoomRole = member.RoomRole
					// Override role for Casbin matcher to check room-level roles (owner/moderator/member/guest)
					if sysRole != "admin" && member.RoomRole != "" {
						sub.Role = member.RoomRole
					}
				}
			} else {
				// Not a member of this room
				if sysRole != "admin" {
					sub.Role = "guest"
				}
			}
		}

		// 3. Evaluate permissions with Casbin
		if !enforcer.Can(sub, obj, action) {
			response.Error(c, http.StatusForbidden, "FORBIDDEN", "Permission denied")
			c.Abort()
			return
		}

		c.Next()
	}
}
