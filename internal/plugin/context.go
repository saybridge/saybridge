package plugin

import (
	"context"

	"github.com/gin-gonic/gin"
	natspkg "github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin/actionregistry"
	"github.com/saybridge/saybridge/pkg/config"
)

// PluginContext is the typed set of dependencies handed to native plugins when
// the OnServerStart hook fires. It replaces fragile, repeated
// `payload["db"].(*gorm.DB)` assertions with a single, compile-time-checked
// surface. Build one at the top of a plugin's OnServerStart handler with
// ContextFrom(payload).
//
// Optional dependencies may be nil (e.g. a plugin invoked in a context where a
// given service was not wired), so callers should nil-check anything they don't
// strictly require — exactly as the untyped map access did before.
type PluginContext struct {
	// Infrastructure
	API     *gin.RouterGroup
	DB      *gorm.DB
	RDB     *redis.Client
	JS      natspkg.JetStreamContext
	Cfg     *config.Config
	Proxy   *PluginProxyRouter
	Actions *actionregistry.ActionRegistry

	// Repositories / use cases
	MessageRepo domain.MessageRepository
	MessageUC   domain.MessageUseCase
	UserRepo    domain.UserRepository

	// Core callbacks (bot-identity actions wired by the server).
	SendMessageFn          func(ctx context.Context, senderID, senderName, roomID, content string) (string, error)
	EditMessageFn          func(ctx context.Context, userID, roomID, messageID, newContent string) error
	UpdateMessageContentFn func(ctx context.Context, messageID, content string) error
	PublishWSEventFn       func(ctx context.Context, roomID, eventName, data string) error
	DMMessageFn            func(ctx context.Context, fromUserID, toUserID, content string) (string, error)
}

// ContextFrom extracts a typed PluginContext from the untyped OnServerStart
// payload map. Keys that are missing or of an unexpected type yield zero values
// (nil), mirroring the previous comma-ok assertion behavior.
func ContextFrom(payload map[string]interface{}) *PluginContext {
	pc := &PluginContext{}
	pc.API, _ = payload["api"].(*gin.RouterGroup)
	pc.DB, _ = payload["db"].(*gorm.DB)
	pc.RDB, _ = payload["rdb"].(*redis.Client)
	pc.JS, _ = payload["js"].(natspkg.JetStreamContext)
	pc.Cfg, _ = payload["cfg"].(*config.Config)
	pc.Proxy, _ = payload["proxy"].(*PluginProxyRouter)
	pc.Actions, _ = payload["actions"].(*actionregistry.ActionRegistry)
	pc.MessageRepo, _ = payload["message_repo"].(domain.MessageRepository)
	pc.MessageUC, _ = payload["message_uc"].(domain.MessageUseCase)
	pc.UserRepo, _ = payload["user_repo"].(domain.UserRepository)
	pc.SendMessageFn, _ = payload["send_message_fn"].(func(ctx context.Context, senderID, senderName, roomID, content string) (string, error))
	pc.EditMessageFn, _ = payload["edit_message_fn"].(func(ctx context.Context, userID, roomID, messageID, newContent string) error)
	pc.UpdateMessageContentFn, _ = payload["update_message_content_fn"].(func(ctx context.Context, messageID, content string) error)
	pc.PublishWSEventFn, _ = payload["publish_ws_event_fn"].(func(ctx context.Context, roomID, eventName, data string) error)
	pc.DMMessageFn, _ = payload["dm_message_fn"].(func(ctx context.Context, fromUserID, toUserID, content string) (string, error))
	return pc
}
