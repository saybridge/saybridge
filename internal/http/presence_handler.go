package httphandler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/events"
	"github.com/saybridge/saybridge/pkg/response"
)

type PresenceHandler struct {
	repo     domain.PresenceRepository
	userRepo domain.UserRepository
	js       nats.JetStreamContext
	hooks    *plugin.HookRegistry
}

// NewPresenceHandler instantiates a new PresenceHandler controller.
func NewPresenceHandler(repo domain.PresenceRepository, userRepo domain.UserRepository, js nats.JetStreamContext, hooks *plugin.HookRegistry) *PresenceHandler {
	return &PresenceHandler{repo: repo, userRepo: userRepo, js: js, hooks: hooks}
}

type presenceRequest struct {
	Status string `json:"status" binding:"required"` // 'online', 'away', 'busy', 'offline'
}

// UpdatePresence updates a user's presence state and broadcasts the change globally.
// @Summary Update user presence
// @Description Set the user's online status (online, away, busy, offline) and broadcast to all connected clients
// @Tags Presence
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body presenceRequest true "Presence status"
// @Success 200 {object} response.SuccessResponse "Presence updated"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Cache error"
// @Router /api/v1/users/presence [post]
func (h *PresenceHandler) UpdatePresence(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}
	userID := userIDVal.(string)

	tenantIDVal, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Tenant context missing")
		return
	}
	tenantID := tenantIDVal.(string)

	var req presenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	// 1. Get old status for hook payload
	oldStatus := "offline"
	user, err := h.userRepo.GetUserByID(context.Background(), userID)
	if err == nil {
		oldStatus = user.Presence
	}

	// 2. Cache presence status in Redis
	if err := h.repo.SetPresence(context.Background(), userID, req.Status); err != nil {
		response.Error(c, http.StatusInternalServerError, "CACHE_ERROR", err.Error())
		return
	}

	// 3. Persist in relational DB
	if user != nil {
		user.Presence = req.Status
		_ = h.userRepo.UpdateUser(context.Background(), user)
	}

	// 4. Broadcast to JetStream cluster topic
	subject := events.PresenceSubject(tenantID)
	payload := map[string]interface{}{
		"event":   "user:presence:changed",
		"user_id": userID,
		"status":  req.Status,
	}
	_ = events.PublishJSON(h.js, subject, payload)

	// 5. Emit OnUserStatusChange lifecycle hook asynchronously (analytics, external sync, etc.)
	h.hooks.EmitAsync(context.Background(), plugin.OnUserStatusChange, map[string]interface{}{
		"user_id":    userID,
		"old_status": oldStatus,
		"new_status": req.Status,
	})

	response.JSON(c, http.StatusOK, gin.H{"success": true})
}
