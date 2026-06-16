package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeadersConfig holds the configuration for the security headers middleware.
type SecurityHeadersConfig struct {
	ContentSecurityPolicy  string // CSP header value. Empty = default restrictive policy.
	PermissionsPolicy      string // Permissions-Policy header. Empty = restrictive default.
	AllowedHosts           []string // If non-empty, reject requests to unlisted Host headers.
	IsDev                  bool   // Relaxes some headers for development mode.
}

// SecurityHeaders adds comprehensive security headers to every HTTP response.
// Based on OWASP Secure Headers Project recommendations.
func SecurityHeaders(cfg SecurityHeadersConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()

		// Prevent MIME type sniffing
		h.Set("X-Content-Type-Options", "nosniff")

		// Clickjacking protection — SAMEORIGIN allows plugin iframes from same domain
		h.Set("X-Frame-Options", "SAMEORIGIN")

		// XSS filter (legacy browsers)
		h.Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy — only send origin for cross-origin requests
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Prevent caching of sensitive pages (API responses)
		h.Set("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
		h.Set("Pragma", "no-cache")

		// Content Security Policy
		if cfg.ContentSecurityPolicy != "" {
			h.Set("Content-Security-Policy", cfg.ContentSecurityPolicy)
		} else if !cfg.IsDev {
			h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' wss: ws:; font-src 'self'; object-src 'none'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		}

		// HTTP Strict Transport Security (only in production)
		if !cfg.IsDev {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		}

		// Permissions Policy (formerly Feature-Policy)
		if cfg.PermissionsPolicy != "" {
			h.Set("Permissions-Policy", cfg.PermissionsPolicy)
		} else {
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		}

		// Prevent DNS prefetch leaking
		h.Set("X-DNS-Prefetch-Control", "off")

		// Remove Server header to prevent fingerprinting
		h.Del("Server")

		// Host header validation
		if len(cfg.AllowedHosts) > 0 {
			requestHost := c.Request.Host
			allowed := false
			for _, host := range cfg.AllowedHosts {
				if strings.EqualFold(requestHost, host) {
					allowed = true
					break
				}
			}
			if !allowed {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": fmt.Sprintf("Host '%s' is not allowed", requestHost),
				})
				return
			}
		}

		c.Next()
	}
}

// MaxBodySize middleware limits the maximum request body size to prevent denial-of-service.
func MaxBodySize(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": fmt.Sprintf("Request body too large. Maximum allowed: %d bytes", maxBytes),
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}

// SanitizeInput provides basic XSS prevention by checking common injection patterns.
// This is a defense-in-depth measure; proper output encoding is the primary XSS defense.
func SanitizeInput() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check query parameters for obvious script injection
		for key, values := range c.Request.URL.Query() {
			for _, val := range values {
				if containsXSSPattern(val) {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
						"error": fmt.Sprintf("Potentially unsafe input detected in query parameter '%s'", key),
					})
					return
				}
			}
		}
		c.Next()
	}
}

// containsXSSPattern checks for common XSS attack patterns in input strings.
func containsXSSPattern(input string) bool {
	lower := strings.ToLower(input)
	patterns := []string{
		"<script",
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
		"onmouseover=",
		"onfocus=",
		"eval(",
		"document.cookie",
		"document.write",
		"window.location",
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
