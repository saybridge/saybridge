// Package apperr provides a unified error handling system for the application.
// Errors carry HTTP status codes, error codes, and user-facing messages.
// Handlers use errors.As() to extract AppError and send consistent JSON responses.
package apperr

import (
	"fmt"
	"net/http"
)

// AppError is a structured application error that carries HTTP status and error code.
type AppError struct {
	// HTTP status code to return to the client.
	Status int `json:"-"`

	// Machine-readable error code (e.g., "AUTH_INVALID_CREDENTIALS", "ROOM_NOT_FOUND").
	Code string `json:"code"`

	// Human-readable error message for the client.
	Message string `json:"message"`

	// Internal error for logging (not exposed to client).
	Internal error `json:"-"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Internal != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Internal)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the internal error for errors.Is/errors.As chain support.
func (e *AppError) Unwrap() error {
	return e.Internal
}

// ── Factory Functions ────────────────────────────────────────────────────────

// New creates a new AppError with the given status, code, and message.
func New(status int, code, message string) *AppError {
	return &AppError{Status: status, Code: code, Message: message}
}

// Wrap wraps an internal error with an AppError context.
func Wrap(status int, code, message string, err error) *AppError {
	return &AppError{Status: status, Code: code, Message: message, Internal: err}
}

// ── Common Errors ────────────────────────────────────────────────────────────

// 400 Bad Request
func BadRequest(code, message string) *AppError {
	return New(http.StatusBadRequest, code, message)
}

// 401 Unauthorized
func Unauthorized(message string) *AppError {
	return New(http.StatusUnauthorized, "AUTH_UNAUTHORIZED", message)
}

// 403 Forbidden
func Forbidden(message string) *AppError {
	return New(http.StatusForbidden, "AUTH_FORBIDDEN", message)
}

// 404 Not Found
func NotFound(resource, id string) *AppError {
	return New(http.StatusNotFound, resource+"_NOT_FOUND",
		fmt.Sprintf("%s '%s' not found", resource, id))
}

// 409 Conflict
func Conflict(code, message string) *AppError {
	return New(http.StatusConflict, code, message)
}

// 422 Unprocessable Entity (validation errors)
func Validation(message string) *AppError {
	return New(http.StatusUnprocessableEntity, "VALIDATION_ERROR", message)
}

// 429 Too Many Requests
func RateLimited() *AppError {
	return New(http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, please try again later")
}

// 500 Internal Server Error
func Internal(message string, err error) *AppError {
	return Wrap(http.StatusInternalServerError, "INTERNAL_ERROR", message, err)
}

// ── Pre-defined Errors ──────────────────────────────────────────────────────

var (
	ErrInvalidCredentials = New(http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "invalid email or password")
	ErrTokenExpired       = New(http.StatusUnauthorized, "AUTH_TOKEN_EXPIRED", "token has expired")
	ErrSessionNotFound    = New(http.StatusUnauthorized, "AUTH_SESSION_NOT_FOUND", "session not found or expired")
	ErrAccountDisabled    = New(http.StatusForbidden, "AUTH_ACCOUNT_DISABLED", "user account has been disabled")
	ErrRoomNotFound       = New(http.StatusNotFound, "ROOM_NOT_FOUND", "room not found")
	ErrNotRoomMember      = New(http.StatusForbidden, "ROOM_NOT_MEMBER", "access denied: not a member of this room")
	ErrAdminRequired      = New(http.StatusForbidden, "AUTH_ADMIN_REQUIRED", "admin access required")
	ErrMessageNotFound    = New(http.StatusNotFound, "MESSAGE_NOT_FOUND", "message not found")
	ErrUserNotFound       = New(http.StatusNotFound, "USER_NOT_FOUND", "user not found")
)
