package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/pkg/response"
	"github.com/rs/zerolog/log"
)

// RecoveryMiddleware intercepts uncaught panics inside GIN handlers to protect the process.
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Emit panic details along with stacktrace dump
				log.Error().
					Interface("panic_err", err).
					Str("stack", string(debug.Stack())).
					Msg("Successfully recovered from critical server panic!")

				// Return standardized HTTP 500 error payload
				response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", fmt.Sprintf("A critical server error occurred: %v", err))
			}
		}()
		c.Next()
	}
}
