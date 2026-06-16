package ws

import (
	"context"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/http/middleware"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/crypto"
	"github.com/redis/go-redis/v9"
)

// newUpgrader creates a WebSocket upgrader that validates the Origin header
// against the provided allowed origins list. When allowedOrigins is nil every
// origin is accepted (development mode).
func newUpgrader(allowedOrigins []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				// Non-browser clients (e.g. curl, Postman) may not send Origin
				return true
			}
			return middleware.IsOriginAllowed(origin, allowedOrigins)
		},
	}
}

// ServeWS handles protocol upgrade handshake requests, verifies JWT token parameters, and boots Pumps.
func ServeWS(hub *Hub, jwtMgr *crypto.JWTManager, msgUseCase domain.MessageUseCase, rdb *redis.Client, userRepo domain.UserRepository, js nats.JetStreamContext, hooks *plugin.HookRegistry, allowedOrigins []string) gin.HandlerFunc {
	wsUpgrader := newUpgrader(allowedOrigins)

	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   gin.H{"code": "UNAUTHORIZED", "message": "Authentication token query is required"},
			})
			return
		}

		// Verify Access Token signature and claims payload
		claims, err := jwtMgr.VerifyAccessToken(token)
		if err != nil {
			log.Printf("[WSS Auth] Verification failed: %v", err)
			// Return formal system error payload complying with blueprint specs
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   gin.H{"code": "auth:expired", "message": "Access token has expired or is invalid: " + err.Error()},
			})
			return
		}

		// Upgrade HTTP session to persistent TCP WebSocket protocol
		conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[WSS Upgrade] Failed to handshake connection: %v", err)
			return
		}

		username := ""
		u, err := userRepo.GetUserByID(context.Background(), claims.Subject)
		if err == nil && u != nil {
			username = u.Username
		}

		client := &Client{
			hub:        hub,
			conn:       conn,
			send:       make(chan []byte, 256),
			userID:     claims.Subject,
			username:   username,
			tenantID:   claims.TenantID,
			deviceID:   claims.DeviceID,
			role:       claims.Role,
			limiter:    NewRateLimiter(0.5, 30.0), // Refill rate: 0.5 tokens/sec, capacity: 30 tokens (max 30 msg/min)
			msgUseCase: msgUseCase,
			rdb:        rdb,
			userRepo:   userRepo,
			js:         js,
			hooks:      hooks,
		}

		// Mark user online
		client.setUserPresence(context.Background(), "online")

		// Register client connection to coordinate Hub
		client.hub.register <- client

		// Launch active pumps in separate non-blocking execution threads
		go client.WritePump()
		go client.ReadPump()
	}
}
