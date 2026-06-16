package httphandler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
)

// MessageHandler handles HTTP REST requests for fetching messaging histories.
type MessageHandler struct {
	useCase domain.MessageUseCase
}

// NewMessageHandler instantiates a new MessageHandler controller.
func NewMessageHandler(useCase domain.MessageUseCase) *MessageHandler {
	return &MessageHandler{useCase: useCase}
}

// GetMessageHistory returns a cursor-paginated array of messages for the specified room.
// @Summary Get message history
// @Description Retrieve paginated message history for a room using cursor-based pagination
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Param roomId path string true "Room ID (UUID)"
// @Param limit query integer false "Number of messages to return" default(50)
// @Param before_id query string false "Cursor: return messages before this message ID"
// @Success 200 {object} response.SuccessResponse "Message list with pagination cursor"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 403 {object} response.ErrorResponse "Access denied"
// @Router /api/v1/messages/room/{roomId} [get]
func (h *MessageHandler) GetMessageHistory(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	roomID := c.Param("roomId")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID path parameter is required")
		return
	}

	// Parse limit (pagination size)
	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil {
			limit = val
		}
	}

	beforeID := c.Query("before_id")

	messages, err := h.useCase.GetMessageHistory(c.Request.Context(), userID, roomID, limit, beforeID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "ACCESS_DENIED", err.Error())
		return
	}

	if messages == nil {
		messages = []domain.Message{}
	}

	// Build pagination metadata
	var lastMessageID string
	hasMore := len(messages) >= limit
	if len(messages) > 0 {
		lastMessageID = messages[len(messages)-1].MessageID
	}

	response.JSONWithMeta(c, http.StatusOK, messages, gin.H{
		"cursor":   lastMessageID,
		"has_more": hasMore,
	})
}

// GetThreadReplies returns all replies in a nested message thread.
// @Summary Get thread replies
// @Description Retrieve all replies in a message thread
// @Tags Messages
// @Produce json
// @Security BearerAuth
// @Param parentId path string true "Parent message ID (UUID)"
// @Param room_id query string true "Room ID containing the thread"
// @Success 200 {object} response.SuccessResponse "Thread replies"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 403 {object} response.ErrorResponse "Access denied"
// @Router /api/v1/messages/thread/{parentId}/replies [get]
func (h *MessageHandler) GetThreadReplies(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	parentID := c.Param("parentId")
	if parentID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Parent ID path parameter is required")
		return
	}

	roomID := c.Query("room_id")
	if roomID == "" {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", "Room ID query parameter is required")
		return
	}

	replies, err := h.useCase.GetThreadReplies(c.Request.Context(), userID, roomID, parentID)
	if err != nil {
		response.Error(c, http.StatusForbidden, "ACCESS_DENIED", err.Error())
		return
	}

	if replies == nil {
		replies = []domain.Message{}
	}

	response.JSON(c, http.StatusOK, replies)
}
