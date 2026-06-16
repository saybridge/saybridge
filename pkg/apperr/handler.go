package apperr

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorResponse is the standard JSON error response format.
type ErrorResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
}

// HandleError inspects the error and sends an appropriate JSON response.
// If the error is an *AppError, it uses the structured status/code/message.
// Otherwise it sends a generic 500 Internal Server Error.
//
// Usage in handlers:
//
//	result, err := usecase.DoSomething(ctx, ...)
//	if err != nil {
//	    apperr.HandleError(c, err)
//	    return
//	}
func HandleError(c *gin.Context, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		// Log internal error if present
		if appErr.Internal != nil {
			log.Printf("[AppError] %s: %v", appErr.Code, appErr.Internal)
		}

		c.JSON(appErr.Status, ErrorResponse{
			Success: false,
			Error:   appErr.Message,
			Code:    appErr.Code,
		})
		return
	}

	// Unstructured error — log and return generic 500
	log.Printf("[Error] Unhandled error: %v", err)
	c.JSON(http.StatusInternalServerError, ErrorResponse{
		Success: false,
		Error:   "internal server error",
		Code:    "INTERNAL_ERROR",
	})
}

// AbortWithError is like HandleError but also calls c.Abort().
// Use in middleware where you want to stop the handler chain.
func AbortWithError(c *gin.Context, err error) {
	HandleError(c, err)
	c.Abort()
}
