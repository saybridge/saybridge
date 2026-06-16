package httphandler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
)

// AuthHandler handles HTTP requests related to user registration, login, token refresh, and logout.
type AuthHandler struct {
	useCase domain.AuthUseCase
}

// NewAuthHandler instantiates a new AuthHandler controller.
func NewAuthHandler(useCase domain.AuthUseCase) *AuthHandler {
	return &AuthHandler{useCase: useCase}
}

// RegisterRequest holds input validation rules for creating a user.
type RegisterRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=50"`
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name" binding:"required,max=100"`
}

// Register handles user sign up requests.
// @Summary Register a new user
// @Description Register a new user with username, email, password, and display name
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Registration details"
// @Success 201 {object} response.SuccessResponse "Registration successful"
// @Failure 400 {object} response.ErrorResponse "Invalid inputs"
// @Failure 409 {object} response.ErrorResponse "Registration fail (conflict)"
// @Router /api/v1/auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	user, err := h.useCase.Register(c.Request.Context(), req.Username, req.Email, req.Password, req.DisplayName)
	if err != nil {
		response.Error(c, http.StatusConflict, "REGISTRATION_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusCreated, user)
}

// LoginRequest holds credentials and device identification for signing in.
type LoginRequest struct {
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required"`
	DeviceID   string `json:"device_id" binding:"required"`
	DeviceName string `json:"device_name"`
}

// Login handles credentials validation and issues token pairs.
// @Summary Login to account
// @Description Login user and generate access & refresh tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login credentials"
// @Success 200 {object} response.SuccessResponse "Login successful"
// @Failure 400 {object} response.ErrorResponse "Invalid inputs"
// @Failure 401 {object} response.ErrorResponse "Auth failure"
// @Router /api/v1/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	ipAddress := c.ClientIP()
	userAgent := c.Request.UserAgent()

	tokens, err := h.useCase.Login(c.Request.Context(), req.Email, req.Password, req.DeviceID, req.DeviceName, ipAddress, userAgent)
	if err != nil {
		if len(err.Error()) > 13 && err.Error()[:13] == "2fa_required:" {
			tempToken := err.Error()[13:]
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data": gin.H{
					"2fa_required": true,
					"temp_token":   tempToken,
				},
			})
			return
		}
		response.Error(c, http.StatusUnauthorized, "AUTHENTICATION_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, tokens)
}

// RefreshRequest binds inputs required to renew an access token.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
	DeviceID     string `json:"device_id" binding:"required"`
}

// Refresh processes session token rotation and access token renewals.
// @Summary Refresh access token
// @Description Refresh session tokens using a valid refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshRequest true "Token refresh credentials"
// @Success 200 {object} response.SuccessResponse "Token refreshed"
// @Failure 400 {object} response.ErrorResponse "Invalid inputs"
// @Failure 401 {object} response.ErrorResponse "Refresh failure"
// @Router /api/v1/auth/refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	tokens, err := h.useCase.Refresh(c.Request.Context(), req.RefreshToken, req.DeviceID)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, "REFRESH_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, tokens)
}

// LogoutRequest holds the opaque session token to be revoked.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// Logout processes user logout by destroying the active session.
// @Summary Logout from session
// @Description Terminate the user session associated with the refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LogoutRequest true "Logout session details"
// @Success 200 {object} response.SuccessResponse "Logout successful"
// @Failure 400 {object} response.ErrorResponse "Invalid inputs"
// @Failure 500 {object} response.ErrorResponse "Logout failure"
// @Router /api/v1/auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	var req LogoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	err := h.useCase.Logout(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "LOGOUT_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{"message": "successfully logged out"})
}
