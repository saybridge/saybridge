package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/plugin/actionregistry"
	"github.com/saybridge/saybridge/internal/app"
	"github.com/saybridge/saybridge/internal/plugin/wasm"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/events"
)

// RegisterPluginRoutes sets up plugin system routes and wires the plugin Dependencies struct.
func RegisterPluginRoutes(api *gin.RouterGroup, s *Server, c *app.Container) {
	// Plugin system — scan plugins/ directory for WASM plugins
	manifestHandler := plugin.NewManifestHandler(s.rdb, s.natsConn)
	deps := plugin.Dependencies{
		DB: s.db, RDB: s.rdb, JWTMgr: s.jwtMgr,
		Cfg: s.cfg, Hooks: plugin.Registry,
		SendMessageFn: func(ctx context.Context, roomID, content, senderName string) error {
			// Create message directly — bypass user lookup & room membership checks
			msg := &domain.Message{
				RoomID:     roomID,
				SenderID:   domain.SystemActorID,
				SenderName: senderName,
				Content:    content,
				MsgType:    "text",
				CreatedAt:  time.Now(),
			}
			if err := c.MessageRepo.SaveMessage(ctx, msg); err != nil {
				return err
			}
			// Broadcast via NATS so clients receive it in real-time
			subject := events.RoomSubject(domain.DefaultTenantID, roomID)
			payload := map[string]interface{}{
				"event":   "msg:receive",
				"room_id": roomID,
				"data":    msg,
			}
			_ = events.PublishJSON(s.js, subject, payload)
			return nil
		},
		DeleteMessageFn: func(ctx context.Context, roomID string, timeBucket int, messageID string) error {
			msg, err := c.MessageRepo.GetMessage(ctx, roomID, timeBucket, messageID)
			if err != nil {
				return err
			}
			msg.IsDeleted = true
			msg.IsEdited = true
			msg.EditedAt = time.Now()
			if err := c.MessageRepo.UpdateMessage(ctx, msg); err != nil {
				return err
			}
			// Broadcast via NATS so clients receive it in real-time
			subject := events.RoomSubject(domain.DefaultTenantID, roomID)
			payload := map[string]interface{}{
				"event":   "msg:receive",
				"room_id": roomID,
				"data":    msg,
			}
			_ = events.PublishJSON(s.js, subject, payload)
			return nil
		},
		GetRoomHistoryFn: func(ctx context.Context, roomID string) (string, error) {
			messages, err := c.MessageRepo.GetMessageHistory(ctx, roomID, 50, "")
			if err != nil {
				return "", err
			}
			var sb strings.Builder
			for i := len(messages) - 1; i >= 0; i-- {
				m := messages[i]
				if m.IsDeleted {
					continue
				}
				sb.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
			}
			return sb.String(), nil
		},
		PublishWSEventFn: func(ctx context.Context, roomID, eventName, data string) error {
			subject := events.RoomSubject(domain.DefaultTenantID, roomID)
			payload := map[string]interface{}{
				"event":   eventName,
				"room_id": roomID,
				"data":    json.RawMessage(data),
			}
			return events.PublishJSON(s.js, subject, payload)
		},
		EditMessageFn: func(ctx context.Context, userID, roomID, messageID, newContent string) error {
			// TimescaleDB doesn't need timeBucket — pass 0
			_, err := c.MessageUC.EditMessage(ctx, domain.DefaultTenantID, userID, roomID, messageID, 0, newContent)
			return err
		},
		CreateRoomFn: func(ctx context.Context, name, roomType string) (string, error) {
			room, err := c.RoomUC.CreateRoom(ctx, domain.DefaultTenantID, domain.SystemActorID, name, roomType, "", "", false)
			if err != nil {
				return "", err
			}
			return room.ID, nil
		},
		GetUserFn: func(ctx context.Context, userID string) (string, error) {
			user, err := c.UserRepo.GetUserByID(ctx, userID)
			if err != nil {
				return "", err
			}
			userBytes, err := json.Marshal(user)
			if err != nil {
				return "", err
			}
			return string(userBytes), nil
		},
		GetRoomMembersFn: func(ctx context.Context, roomID string) (string, error) {
			room, err := c.RoomRepo.GetRoomByID(ctx, roomID)
			if err != nil {
				return "", err
			}
			membersBytes, err := json.Marshal(room.Members)
			if err != nil {
				return "", err
			}
			return string(membersBytes), nil
		},
		AddReactionFn: func(ctx context.Context, roomID, messageID, emoji string) error {
			// TimescaleDB doesn't need timeBucket — pass 0
			_, err := c.MessageUC.ToggleReaction(ctx, domain.DefaultTenantID, domain.SystemActorID, roomID, messageID, 0, emoji)
			return err
		},
		RegisterSlashCommandFn: func(ctx context.Context, command, description string) error {
			return s.rdb.HSet(ctx, "plugin:slash_commands", command, description).Err()
		},
		ScheduleTimerFn: func(ctx context.Context, delayMs int32, hookEvent string, payload string) error {
			go func() {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
				var payloadMap map[string]interface{}
				if err := json.Unmarshal([]byte(payload), &payloadMap); err == nil {
					plugin.Registry.EmitAsync(context.Background(), plugin.HookEvent(hookEvent), payloadMap)
				}
			}()
			return nil
		},
		Proxy:       plugin.DefaultProxyRouter,
		Actions:     actionregistry.DefaultRegistry,
		MessageRepo: c.MessageRepo,
	}

	wasm.ScanAndRegister("plugins", &deps, manifestHandler)
	api.GET("/plugins/manifest", manifestHandler.GetManifests)
	api.POST("/plugins/:slug/toggle", manifestHandler.TogglePlugin)

	// Proxy plugin API requests through dynamic proxy router
	api.Any("/plugins/:slug/api/*path", plugin.DefaultProxyRouter.ServeHTTP)
}
