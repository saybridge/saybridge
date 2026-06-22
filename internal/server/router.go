package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/app"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/http/middleware"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/internal/plugin/actionregistry"
	"github.com/saybridge/saybridge/internal/ws"
	"github.com/saybridge/saybridge/pkg/events"
	"github.com/saybridge/saybridge/pkg/metrics"

	_ "github.com/saybridge/saybridge/docs"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// SetupRouter configures the Gin engine with routes, middleware, and handler wiring.
// Receives a fully-initialized Server with all dependencies ready.
func SetupRouter(s *Server) *gin.Engine {
	r := gin.New()

	// ── Container lifecycle ──────────────────────────────────────────────────
	c := app.NewContainer(s.cfg, s.db, s.rdb, s.js, s.natsConn, s.meili, s.jwtMgr)
	c.Register()
	c.Boot()

	// ── Authorization ────────────────────────────────────────────────────────
	enforcer := initEnforcer()
	c.RoomH.SetEnforcer(enforcer)
	c.MessageUC.SetEnforcer(enforcer)

	// ── Parse allowed CORS origins from config ──────────────────────────────
	allowedOrigins := middleware.ParseCORSOrigins(s.cfg.CORSOrigins)

	// ── Global Middleware ────────────────────────────────────────────────────
	r.Use(middleware.LoggerMiddleware())
	r.Use(middleware.RecoveryMiddleware())
	r.Use(metrics.PrometheusMiddleware())
	r.Use(middleware.CORSMiddlewareWithOrigins(allowedOrigins))
	r.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{
		IsDev: s.cfg.Env == "" || s.cfg.Env == "development",
	}))
	r.Use(middleware.MaxBodySize(10 << 20)) // 10 MB max request body

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ── Prometheus metrics (scrape target; guard with METRICS_TOKEN env) ─────
	r.GET("/metrics", metrics.MetricsHandler())

	// Register DB connection-pool collector for pool stats in /metrics.
	if sqlDB, err := s.db.DB(); err == nil {
		metrics.RegisterDBCollector(sqlDB, "main")
	}

	// ── System routes (Public) ───────────────────────────────────────────────
	RegisterSystemRoutes(r, s.db)

	// ── WebSocket (Public with token handshake) ──────────────────────────────
	r.GET("/api/v1/ws", ws.ServeWS(c.Hub, c.JWTMgr, c.MessageUC, c.RDB, c.UserRepo, c.JS, plugin.Registry, allowedOrigins))

	// ── Auth routes (Public) ─────────────────────────────────────────────────
	RegisterAuthRoutes(r, c, s.rdb)

	// ── Plugin static files (Public — HTML/CSS/JS for plugin iframes) ─────
	r.GET("/plugins/:slug/static/*filepath", func(ctx *gin.Context) {
		slug := ctx.Param("slug")
		raw := ctx.Param("filepath")
		fp := strings.TrimPrefix(raw, "/")
		if fp == "" {
			fp = "index.html"
		}
		cleaned := filepath.Clean(fp)
		if strings.Contains(cleaned, "..") {
			ctx.JSON(400, gin.H{"error": "invalid path"})
			return
		}
		fullPath := filepath.Join("plugins", slug, "web", cleaned)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			fullPath = filepath.Join("backend", "plugins", slug, "web", cleaned)
		}
		ctx.File(fullPath)
	})

	// ── Protected API (JWT required) ─────────────────────────────────────────
	api := r.Group("/api/v1")
	api.Use(middleware.AuthMiddleware(s.jwtMgr, s.rdb))
	api.Use(middleware.DynamicRateLimitMiddleware(s.rdb))

	// ── Notification hook ────────────────────────────────────────────────────
	RegisterNotificationHook(s, c)

	// ── Route modules ────────────────────────────────────────────────────────
	RegisterPluginRoutes(api, s, c)
	RegisterUserRoutes(api, c)
	RegisterRoomRoutes(api, s, c, enforcer)
	RegisterMessageRoutes(api, c)
	RegisterSearchRoutes(api, c)
	RegisterAdminRoutes(api, s, c, enforcer)

	// ── OnServerStart hook ───────────────────────────────────────────────────
	// Fire OnServerStart so native plugins can register routes & init
	plugin.Registry.Emit(context.Background(), plugin.OnServerStart, map[string]interface{}{
		"api":          api,
		"db":           s.db,
		"rdb":          s.rdb,
		"js":           s.js,
		"cfg":          s.cfg,
		"message_repo": c.MessageRepo,
		"message_uc":   c.MessageUC,
		"proxy":        plugin.DefaultProxyRouter,
		"actions":      actionregistry.DefaultRegistry,
		"user_repo":    c.UserRepo,
		"send_message_fn": func(ctx context.Context, senderID, senderName, roomID, content string) (string, error) {
			if senderID == "" {
				senderID = domain.SystemActorID
			}
			if senderName == "" {
				senderName = "AI Assistant"
			}
			msg := &domain.Message{
				RoomID:     roomID,
				SenderID:   senderID,
				SenderName: senderName,
				Content:    content,
				MsgType:    "text",
				CreatedAt:  time.Now(),
			}
			if err := c.MessageRepo.SaveMessage(ctx, msg); err != nil {
				return "", err
			}
			subject := events.RoomSubject(domain.DefaultTenantID, roomID)
			_ = events.PublishJSON(s.js, subject, map[string]interface{}{
				"event": "msg:receive", "room_id": roomID, "data": msg,
			})
			return msg.MessageID, nil
		},
		"edit_message_fn": func(ctx context.Context, userID, roomID, messageID, newContent string) error {
			_, err := c.MessageUC.EditMessage(ctx, domain.DefaultTenantID, userID, roomID, messageID, 0, newContent)
			return err
		},
		"update_message_content_fn": func(ctx context.Context, messageID, content string) error {
			return c.MessageRepo.UpdateMessageContent(ctx, messageID, content)
		},
		// dm_message_fn delivers a message into the direct room between two users,
		// creating it on first use. Used for AI-initiated DMs (e.g. catch-up digests).
		"dm_message_fn": func(ctx context.Context, fromUserID, toUserID, content string) (string, error) {
			room, err := c.RoomUC.CreateRoom(ctx, domain.DefaultTenantID, fromUserID, toUserID, "direct", "", "", false)
			if err != nil {
				return "", err
			}
			msg := &domain.Message{
				RoomID:     room.ID,
				SenderID:   fromUserID,
				SenderName: "AI Assistant",
				Content:    content,
				MsgType:    "text",
				CreatedAt:  time.Now(),
			}
			if err := c.MessageRepo.SaveMessage(ctx, msg); err != nil {
				return "", err
			}
			subject := events.RoomSubject(domain.DefaultTenantID, room.ID)
			_ = events.PublishJSON(s.js, subject, map[string]interface{}{
				"event": "msg:receive", "room_id": room.ID, "data": msg,
			})
			return msg.MessageID, nil
		},
		"publish_ws_event_fn": func(ctx context.Context, roomID, eventName, data string) error {
			subject := events.RoomSubject(domain.DefaultTenantID, roomID)
			payloadMap := map[string]interface{}{
				"event":   eventName,
				"room_id": roomID,
				"data":    json.RawMessage(data),
			}
			return events.PublishJSON(s.js, subject, payloadMap)
		},
	})

	return r
}
