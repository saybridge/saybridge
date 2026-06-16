package httphandler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/plugin/actionregistry"
	"github.com/saybridge/saybridge/internal/authz"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// RoomHandler (from handler/room.go)
// ---------------------------------------------------------------------------

// RoomHandler handles HTTP endpoints related to chat Room CRUD and Membership operations.
type RoomHandler struct {
	useCase     domain.RoomUseCase
	messageRepo domain.MessageRepository
	enforcer    *authz.AuthzEnforcer
}

// NewRoomHandler instantiates a new RoomHandler controller.
func NewRoomHandler(useCase domain.RoomUseCase, messageRepo domain.MessageRepository, enforcer *authz.AuthzEnforcer) *RoomHandler {
	return &RoomHandler{
		useCase:     useCase,
		messageRepo: messageRepo,
		enforcer:    enforcer,
	}
}

// SetEnforcer sets the Casbin authorization enforcer.
// Used when the enforcer is initialized separately from handler construction (e.g. DI container).
func (h *RoomHandler) SetEnforcer(enforcer *authz.AuthzEnforcer) {
	h.enforcer = enforcer
}


// CreateRoomRequest defines input validations for room creations.
type CreateRoomRequest struct {
	Name        string `json:"name" binding:"max=100"`
	Type        string `json:"type" binding:"required"`
	Description string `json:"description" binding:"max=500"`
	Topic       string `json:"topic" binding:"max=500"`
	IsEncrypted bool   `json:"is_encrypted"`
}

// CreateRoom processes room creation requests.
// @Summary Create a new room
// @Description Create a new chat room (channel, group, or direct message)
// @Tags Rooms
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateRoomRequest true "Room creation details"
// @Success 201 {object} response.SuccessResponse "Room created"
// @Failure 400 {object} response.ErrorResponse "Invalid input or creation failed"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Router /api/v1/rooms [post]
func (h *RoomHandler) CreateRoom(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	tenantID := tenantIDVal.(string)

	var req CreateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	room, err := h.useCase.CreateRoom(c.Request.Context(), tenantID, userID, req.Name, req.Type, req.Description, req.Topic, req.IsEncrypted)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "ROOM_CREATION_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusCreated, room)
}

// ListRooms retrieves all chat rooms of which the authenticated user is currently a member.
// @Summary List user's rooms
// @Description Get all rooms the authenticated user is a member of
// @Tags Rooms
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessResponse "List of rooms"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Fetch failed"
// @Router /api/v1/rooms [get]
func (h *RoomHandler) ListRooms(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	tenantID := tenantIDVal.(string)

	rooms, err := h.useCase.ListRooms(c.Request.Context(), tenantID, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "ROOMS_FETCH_FAILED", err.Error())
		return
	}

	if rooms == nil {
		rooms = []domain.Room{}
	}

	response.JSON(c, http.StatusOK, rooms)
}

// GetRoomDetails retrieves comprehensive settings, metadata, and memberships of a single room.
// @Summary Get room details
// @Description Get detailed information about a specific room including members
// @Tags Rooms
// @Produce json
// @Security BearerAuth
// @Param id path string true "Room ID (UUID)"
// @Success 200 {object} response.SuccessResponse "Room details"
// @Failure 400 {object} response.ErrorResponse "Invalid room ID"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 403 {object} response.ErrorResponse "Access denied"
// @Router /api/v1/rooms/{id} [get]
func (h *RoomHandler) GetRoomDetails(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	room, err := h.useCase.GetRoomDetails(c.Request.Context(), userID, roomID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "ACCESS_DENIED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, room)
}

// GetRoomBySlug resolves a room by its URL-friendly slug.
func (h *RoomHandler) GetRoomBySlug(c *gin.Context) {
	_, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	slug := c.Param("slug")
	if slug == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Slug is required")
		return
	}

	room, err := h.useCase.GetRoomBySlug(c.Request.Context(), domain.DefaultTenantID, slug)
	if err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "Room not found")
		return
	}

	response.JSON(c, http.StatusOK, room)
}

// InviteMemberRequest holds parameters needed to append a new member to a room.
type InviteMemberRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

// InviteMember processes request to append a user as a member to a room.
// @Summary Invite member to room
// @Description Add a user as a member of a room
// @Tags Rooms
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Room ID (UUID)"
// @Param request body InviteMemberRequest true "User to invite"
// @Success 200 {object} response.SuccessResponse "Member added"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 403 {object} response.ErrorResponse "Invite failed"
// @Router /api/v1/rooms/{id}/members [post]
func (h *RoomHandler) InviteMember(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	var req InviteMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	member, err := h.useCase.InviteMember(c.Request.Context(), userID, roomID, req.UserID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "INVITE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, member)
}

// KickMember processes request to forcefully eject an active member from a room.
// @Summary Kick member from room
// @Description Remove a user from room membership
// @Tags Rooms
// @Produce json
// @Security BearerAuth
// @Param id path string true "Room ID (UUID)"
// @Param userId path string true "User ID to kick (UUID)"
// @Success 200 {object} response.SuccessResponse "Member kicked"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 403 {object} response.ErrorResponse "Kick failed"
// @Router /api/v1/rooms/{id}/members/{userId} [delete]
func (h *RoomHandler) KickMember(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("id")
	targetUserID := c.Param("userId")
	if roomID == "" || targetUserID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID and target User ID are required")
		return
	}

	err := h.useCase.KickMember(c.Request.Context(), userID, roomID, targetUserID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "KICK_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "successfully kicked member from room"})
}

// LeaveRoom processes request to willingly exit a room's active membership.
// @Summary Leave a room
// @Description Leave a room as the authenticated user
// @Tags Rooms
// @Produce json
// @Security BearerAuth
// @Param id path string true "Room ID (UUID)"
// @Success 200 {object} response.SuccessResponse "Left room"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 403 {object} response.ErrorResponse "Leave failed"
// @Router /api/v1/rooms/{id}/leave [post]
func (h *RoomHandler) LeaveRoom(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	err := h.useCase.LeaveRoom(c.Request.Context(), userID, roomID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "LEAVE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "successfully left room"})
}

// ToggleE2EERequest defines input for toggling E2EE.
type ToggleE2EERequest struct {
	Enabled bool `json:"enabled"`
}

// ToggleE2EE toggles End-to-End Encryption for a room.
// @Summary Toggle E2EE for a room
// @Tags Rooms
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Room ID"
// @Param request body ToggleE2EERequest true "E2EE toggle"
// @Success 200 {object} response.SuccessResponse "Room updated"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 403 {object} response.ErrorResponse "Unauthorized"
// @Router /api/v1/rooms/{id}/e2ee [put]
func (h *RoomHandler) ToggleE2EE(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	var req ToggleE2EERequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Invalid request body")
		return
	}

	room, err := h.useCase.ToggleE2EE(c.Request.Context(), userID, roomID, req.Enabled)
	if err != nil {
		response.Error(c, http.StatusForbidden, "TOGGLE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, room)
}

// GetActions retrieves user permitted actions for a specific room or message context.
func (h *RoomHandler) GetActions(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}
	userID := userIDVal.(string)

	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	slotStr := c.Query("slot")
	if slotStr == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Slot parameter is required")
		return
	}
	slot := actionregistry.ActionSlot(slotStr)

	messageID := c.Query("message_id")
	timeBucketStr := c.Query("time_bucket")

	room, err := h.useCase.GetRoomDetails(c.Request.Context(), userID, roomID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room not found or access denied")
		return
	}

	var roomRole string
	for _, m := range room.Members {
		if m.UserID == userID {
			roomRole = m.RoomRole
			break
		}
	}
	if roomRole == "" {
		response.Error(c, http.StatusForbidden, "ACCESS_DENIED", "You are not a member of this room")
		return
	}

	systemRole := "user"
	userRoleVal, ok := c.Get("role")
	if ok {
		systemRole = userRoleVal.(string)
	}

	sub := actionregistry.Subject{
		ID:       userID,
		Role:     systemRole,
		RoomRole: roomRole,
	}

	var ownerID string
	if room.CreatedBy != nil {
		ownerID = *room.CreatedBy
	}
	obj := actionregistry.Object{
		Type:       "room",
		ID:         roomID,
		OwnerID:    ownerID,
		RoomType:   room.Type,
		IsReadOnly: room.IsReadOnly,
	}

	if messageID != "" && h.messageRepo != nil {
		var timeBucket int
		if timeBucketStr != "" {
			fmt.Sscanf(timeBucketStr, "%d", &timeBucket)
		}
		if timeBucket == 0 {
			now := time.Now()
			timeBucket = now.Year()*100 + int(now.Month())
		}

		msg, err := h.messageRepo.GetMessage(c.Request.Context(), roomID, timeBucket, messageID)
		if err == nil && msg != nil {
			obj.Type = "message"
			obj.ID = messageID
			obj.OwnerID = msg.SenderID
		}
	}

	actions := actionregistry.DefaultRegistry.GetActions(slot, sub, obj, h.enforcer)

	sort.Slice(actions, func(i, j int) bool {
		return actions[i].SortOrder < actions[j].SortOrder
	})

	response.JSON(c, http.StatusOK, actions)
}

// ---------------------------------------------------------------------------
// RoomMemberHandler (from handler/room_member_handler.go)
// ---------------------------------------------------------------------------

// RoomMemberHandler handles per-member room settings like favorite and mute.
type RoomMemberHandler struct {
	db *gorm.DB
}

// NewRoomMemberHandler creates a new RoomMemberHandler.
func NewRoomMemberHandler(db *gorm.DB) *RoomMemberHandler {
	return &RoomMemberHandler{db: db}
}

// ToggleFavorite toggles the IsFavorite flag for the current user in a room.
// POST /api/v1/rooms/:id/favorite
func (h *RoomMemberHandler) ToggleFavorite(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}
	userID := userIDVal.(string)

	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	var member domain.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&member).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "Room membership not found")
		return
	}

	member.IsFavorite = !member.IsFavorite
	if err := h.db.Save(&member).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"is_favorite": member.IsFavorite,
		"message":     "Favorite toggled",
	})
}

// ToggleMute toggles the NotificationsMuted flag for the current user in a room.
// POST /api/v1/rooms/:id/mute
func (h *RoomMemberHandler) ToggleMute(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}
	userID := userIDVal.(string)

	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	var member domain.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&member).Error; err != nil {
		response.Error(c, http.StatusNotFound, "NOT_FOUND", "Room membership not found")
		return
	}

	member.NotificationsMuted = !member.NotificationsMuted
	if err := h.db.Save(&member).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"notifications_muted": member.NotificationsMuted,
		"message":             "Notifications mute toggled",
	})
}

// ---------------------------------------------------------------------------
// ReadPositionHandler (from handler/read_position_handler.go)
// ---------------------------------------------------------------------------

type ReadPositionHandler struct {
	repo domain.ReadPositionRepository
}

// NewReadPositionHandler instantiates a new ReadPositionHandler.
func NewReadPositionHandler(repo domain.ReadPositionRepository) *ReadPositionHandler {
	return &ReadPositionHandler{repo: repo}
}

type markReadRequest struct {
	LastReadMessageID string `json:"last_read_message_id" binding:"required"`
}

// MarkAsRead resets the unread count for a room to zero for the caller user.
// @Summary Mark room as read
// @Description Mark all messages in a room as read up to a specific message ID
// @Tags Rooms
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Room ID (UUID)"
// @Param request body markReadRequest true "Last read message ID"
// @Success 200 {object} response.SuccessResponse "Marked as read"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Database error"
// @Router /api/v1/rooms/{id}/read [post]
func (h *ReadPositionHandler) MarkAsRead(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID is required")
		return
	}

	var req markReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	rp := &domain.ReadPosition{
		UserID:            userID,
		RoomID:            roomID,
		LastReadMessageID: req.LastReadMessageID,
		UnreadCount:       0,
		UpdatedAt:         time.Now(),
	}

	if err := h.repo.UpdateReadPosition(context.Background(), rp); err != nil {
		response.Error(c, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"success": true})
}
