package httphandler

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
)

// UserHandler handles HTTP requests related to user profile and settings management.
type UserHandler struct {
	useCase     domain.UserUseCase
	storageRepo domain.StorageRepository
}

// NewUserHandler instantiates a new UserHandler controller.
func NewUserHandler(useCase domain.UserUseCase, storageRepo domain.StorageRepository) *UserHandler {
	return &UserHandler{
		useCase:     useCase,
		storageRepo: storageRepo,
	}
}

// UpdateProfileRequest holds input validation rules for updating a user profile.
type UpdateProfileRequest struct {
	DisplayName     *string `json:"display_name" binding:"omitempty,max=100"`
	AvatarURL       *string `json:"avatar_url" binding:"omitempty"`
	CustomStatus    *string `json:"custom_status" binding:"omitempty"`
	CurrentPassword *string `json:"current_password" binding:"omitempty"`
	NewPassword     *string `json:"new_password" binding:"omitempty"`
}

// GetProfile returns the currently authenticated user's profile.
// @Summary Get current user profile
// @Description Returns the authenticated user's profile and settings
// @Tags Users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessResponse "User profile"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 404 {object} response.ErrorResponse "User not found"
// @Router /api/v1/users/me [get]
func (h *UserHandler) GetProfile(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid session identifier")
		return
	}

	user, err := h.useCase.GetProfile(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusNotFound, "USER_NOT_FOUND", "Profile could not be retrieved")
		return
	}

	response.JSON(c, http.StatusOK, user)
}

// UpdateProfile processes profile modification requests.
// @Summary Update user profile
// @Description Update the authenticated user's display name and/or avatar URL
// @Tags Users
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body UpdateProfileRequest true "Profile updates"
// @Success 200 {object} response.SuccessResponse "Updated profile"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Update failed"
// @Router /api/v1/users/me [patch]
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid session identifier")
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	input := domain.UpdateProfileInput{
		DisplayName:     req.DisplayName,
		AvatarURL:       req.AvatarURL,
		CustomStatus:    req.CustomStatus,
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
	}

	user, err := h.useCase.UpdateProfile(c.Request.Context(), userID, input)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "PROFILE_UPDATE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, user)
}

// UpdateAvatar uploads a new avatar file and updates the user's profile with the URL.
// @Summary Upload avatar
// @Description Upload user avatar image file (multipart/form-data)
// @Tags Users
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param avatar formData file true "Avatar image file"
// @Success 200 {object} response.SuccessResponse "Updated profile"
// @Failure 400 {object} response.ErrorResponse "Invalid file"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 500 {object} response.ErrorResponse "Upload failed"
// @Router /api/v1/users/me/avatar [post]
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid session identifier")
		return
	}

	fileHeader, err := c.FormFile("avatar")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_FILE", "Failed to get avatar file from form")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_FILE", "Failed to open avatar file")
		return
	}
	defer file.Close()

	ext := filepath.Ext(fileHeader.Filename)
	objectKey := fmt.Sprintf("avatars/%s-%s%s", userID, uuid.New().String(), ext)
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	downloadURL, err := h.storageRepo.UploadFile(c.Request.Context(), objectKey, file, fileHeader.Size, contentType)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "UPLOAD_FAILED", err.Error())
		return
	}

	// Update the user profile with the new avatar URL
	input := domain.UpdateProfileInput{
		AvatarURL: &downloadURL,
	}
	user, err := h.useCase.UpdateProfile(c.Request.Context(), userID, input)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "PROFILE_UPDATE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, user)
}

// UpdateSettingsRequest holds options for user preferences modification.
type UpdateSettingsRequest struct {
	Language             string `json:"language" binding:"omitempty,max=10"`
	Theme                string `json:"theme" binding:"omitempty,max=20"`
	Timezone             string `json:"timezone" binding:"omitempty,max=50"`
	NotificationsEnabled *bool  `json:"notifications_enabled"`
	DesktopNotifications *bool  `json:"desktop_notifications"`
	NotificationSound    string `json:"notification_sound" binding:"omitempty,max=50"`
	// Personalization
	MessageDensity   string `json:"message_density" binding:"omitempty,oneof=cozy compact"`
	FontSize         *int   `json:"font_size" binding:"omitempty,min=12,max=20"`
	EnterBehavior    string `json:"enter_behavior" binding:"omitempty,oneof=send newline"`
	LinkPreview      *bool  `json:"link_preview"`
	RoomSortOrder    string `json:"room_sort_order" binding:"omitempty,oneof=activity alphabetical unread"`
	ShowReadReceipts *bool  `json:"show_read_receipts"`
	ReduceMotion     *bool  `json:"reduce_motion"`
	AccentColor      string `json:"accent_color" binding:"omitempty,max=20"`
}

// UpdateSettings processes configuration adjustment requests.
// @Summary Update user settings
// @Description Update user preferences such as language, theme, timezone, and notification settings
// @Tags Users
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body UpdateSettingsRequest true "Settings updates"
// @Success 200 {object} response.SuccessResponse "Updated settings"
// @Failure 400 {object} response.ErrorResponse "Invalid input"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Failure 404 {object} response.ErrorResponse "User not found"
// @Failure 500 {object} response.ErrorResponse "Update failed"
// @Router /api/v1/users/me/settings [patch]
func (h *UserHandler) UpdateSettings(c *gin.Context) {
	userIDVal, exists := c.Get("user_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid session identifier")
		return
	}

	var req UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	// Build updates map with only non-zero fields
	updates := make(map[string]interface{})
	if req.Language != "" {
		updates["language"] = req.Language
	}
	if req.Theme != "" {
		updates["theme"] = req.Theme
	}
	if req.Timezone != "" {
		updates["timezone"] = req.Timezone
	}
	if req.NotificationsEnabled != nil {
		updates["notifications_enabled"] = *req.NotificationsEnabled
	}
	if req.DesktopNotifications != nil {
		updates["desktop_notifications"] = *req.DesktopNotifications
	}
	if req.NotificationSound != "" {
		updates["notification_sound"] = req.NotificationSound
	}
	if req.MessageDensity != "" {
		updates["message_density"] = req.MessageDensity
	}
	if req.FontSize != nil {
		updates["font_size"] = *req.FontSize
	}
	if req.EnterBehavior != "" {
		updates["enter_behavior"] = req.EnterBehavior
	}
	if req.LinkPreview != nil {
		updates["link_preview"] = *req.LinkPreview
	}
	if req.RoomSortOrder != "" {
		updates["room_sort_order"] = req.RoomSortOrder
	}
	if req.ShowReadReceipts != nil {
		updates["show_read_receipts"] = *req.ShowReadReceipts
	}
	if req.ReduceMotion != nil {
		updates["reduce_motion"] = *req.ReduceMotion
	}
	if req.AccentColor != "" {
		updates["accent_color"] = req.AccentColor
	}

	settings, err := h.useCase.UpdateSettings(c.Request.Context(), userID, updates)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SETTINGS_UPDATE_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, settings)
}

// SearchUsers searches users by username or display name for @mention autocomplete.
// @Summary Search users
// @Description Search users by query string for @mention autocomplete
// @Tags Users
// @Produce json
// @Security BearerAuth
// @Param q query string true "Search query"
// @Param limit query int false "Max results (default 20)"
// @Success 200 {object} response.SuccessResponse "User list"
// @Failure 401 {object} response.ErrorResponse "Unauthorized"
// @Router /api/v1/users/search [get]
func (h *UserHandler) SearchUsers(c *gin.Context) {
	tenantIDVal, exists := c.Get("tenant_id")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	tenantID := tenantIDVal.(string)
	query := c.Query("q")

	limit := 20
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	users, err := h.useCase.SearchUsers(c.Request.Context(), tenantID, query, limit)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "SEARCH_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, users)
}
