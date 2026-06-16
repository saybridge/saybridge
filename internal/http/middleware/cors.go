package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ParseCORSOrigins splits a comma-separated CORS_ORIGINS value into a slice.
// Returns nil when the value is empty or "*" (meaning "allow all").
func ParseCORSOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil // nil ⇒ allow all origins (dev mode)
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		if o := strings.TrimSpace(p); o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return nil
	}
	return origins
}

// IsOriginAllowed returns true when the origin matches one of the allowed entries,
// or when allowedOrigins is nil (allow-all / dev mode).
func IsOriginAllowed(origin string, allowedOrigins []string) bool {
	if allowedOrigins == nil {
		return true
	}
	for _, o := range allowedOrigins {
		if strings.EqualFold(origin, o) {
			return true
		}
	}
	return false
}

// CORSMiddleware configures standard Cross-Origin Resource Sharing (CORS) rules for API clients.
// allowedOrigins controls which origins are permitted; nil means allow all (development).
func CORSMiddlewareWithOrigins(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if allowedOrigins == nil {
			// Development mode — allow every origin
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else if IsOriginAllowed(origin, allowedOrigins) {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			// Origin not in the whitelist — still process, but don't set the header
			// so the browser will block the response on the client side.
		}

		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH, DELETE")

		// Instantly abort with HTTP 204 No Content for OPTIONS preflight requests
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// CORSMiddleware is the legacy no-arg constructor that allows all origins.
// Kept for backward-compatibility; prefer CORSMiddlewareWithOrigins.
func CORSMiddleware() gin.HandlerFunc {
	return CORSMiddlewareWithOrigins(nil)
}
