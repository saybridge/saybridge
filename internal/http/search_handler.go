package httphandler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
)

// SearchHandler exposes HTTP endpoints for full-text search.
type SearchHandler struct {
	useCase domain.SearchUseCase
}

// NewSearchHandler instantiates a new SearchHandler.
func NewSearchHandler(useCase domain.SearchUseCase) *SearchHandler {
	return &SearchHandler{useCase: useCase}
}

// SearchMessages handles GET /api/v1/search/messages
// @Summary Search messages
// @Description Full-text search across messages in accessible rooms
// @Tags Search
// @Produce json
// @Security BearerAuth
// @Param q query string true "Search query"
// @Param room_id query string false "Filter by room ID"
// @Param limit query integer false "Max results" default(20)
// @Success 200 {object} response.SuccessResponse "Search results"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Search failed"
// @Router /api/v1/search/messages [get]
func (h *SearchHandler) SearchMessages(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	tenantID := tenantIDVal.(string)

	query := c.Query("q")
	roomID := c.Query("room_id")

	limitStr := c.Query("limit")
	limit := 20
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil {
			limit = val
		}
	}

	results, err := h.useCase.SearchMessages(c.Request.Context(), tenantID, userID, roomID, query, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SEARCH_FAILED", err.Error())
		return
	}

	if results == nil {
		results = []domain.Message{}
	}

	response.JSON(c, http.StatusOK, results)
}

// SearchUsers handles GET /api/v1/search/users
// @Summary Search users
// @Description Search for users by username or display name within the tenant
// @Tags Search
// @Produce json
// @Security BearerAuth
// @Param q query string true "Search query"
// @Param limit query integer false "Max results" default(20)
// @Success 200 {object} response.SuccessResponse "Matching users"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Search failed"
// @Router /api/v1/search/users [get]
func (h *SearchHandler) SearchUsers(c *gin.Context) {
	tenantIDVal, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)
	query := c.Query("q")

	limitStr := c.Query("limit")
	limit := 20
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil {
			limit = val
		}
	}

	results, err := h.useCase.SearchUsers(c.Request.Context(), tenantID, query, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SEARCH_FAILED", err.Error())
		return
	}

	if results == nil {
		results = []domain.User{}
	}

	response.JSON(c, http.StatusOK, results)
}

// SearchRooms handles GET /api/v1/search/rooms
// @Summary Search rooms
// @Description Search for rooms by name within accessible rooms for the user
// @Tags Search
// @Produce json
// @Security BearerAuth
// @Param q query string true "Search query"
// @Param limit query integer false "Max results" default(20)
// @Success 200 {object} response.SuccessResponse "Matching rooms"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Search failed"
// @Router /api/v1/search/rooms [get]
func (h *SearchHandler) SearchRooms(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	tenantIDVal, exists2 := c.Get("tenant_id")
	if !exists || !exists2 {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID := userIDVal.(string)
	tenantID := tenantIDVal.(string)
	query := c.Query("q")

	limitStr := c.Query("limit")
	limit := 20
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil {
			limit = val
		}
	}

	results, err := h.useCase.SearchRooms(c.Request.Context(), tenantID, userID, query, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SEARCH_FAILED", err.Error())
		return
	}

	if results == nil {
		results = []domain.Room{}
	}

	response.JSON(c, http.StatusOK, results)
}
