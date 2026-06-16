package response

import (
	"github.com/gin-gonic/gin"
)

// SuccessResponse defines the standard structure for successful API responses.
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
}

// ErrorDetail represents specific business error codes, display messages, and status codes.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}

// ErrorResponse defines the standard payload for failed API responses.
type ErrorResponse struct {
	Success bool        `json:"success"`
	Error   ErrorDetail `json:"error"`
}

// JSON aborts/returns a successful standard JSON response.
func JSON(c *gin.Context, status int, data interface{}) {
	c.JSON(status, SuccessResponse{
		Success: true,
		Data:    data,
	})
}

// JSONWithMeta returns a successful standard JSON response including custom metadata (e.g., cursor info).
func JSONWithMeta(c *gin.Context, status int, data interface{}, meta interface{}) {
	c.JSON(status, SuccessResponse{
		Success: true,
		Data:    data,
		Meta:    meta,
	})
}

// Error returns a formatted standard JSON error response and aborts Gin handler chain.
func Error(c *gin.Context, status int, errorCode string, message string) {
	c.AbortWithStatusJSON(status, ErrorResponse{
		Success: false,
		Error: ErrorDetail{
			Code:    errorCode,
			Message: message,
			Status:  status,
		},
	})
}
