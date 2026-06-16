package notification

import (
	"context"

	"github.com/rs/zerolog/log"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"gorm.io/gorm"
)

type Transport interface {
	Name() string
	Send(ctx context.Context, userID string, notification Notification) error
}

type Notification struct {
	Type   string                 `json:"type"` // "message", "mention", "reaction", "invite"
	Title  string                 `json:"title"`
	Body   string                 `json:"body"`
	RoomID string                 `json:"room_id"`
	Data   map[string]interface{} `json:"data"`
}

type NotificationRouter struct {
	db         *gorm.DB
	hooks      *plugin.HookRegistry
	transports []Transport
}

func NewNotificationRouter(db *gorm.DB, hooks *plugin.HookRegistry, transports ...Transport) *NotificationRouter {
	return &NotificationRouter{
		db:         db,
		hooks:      hooks,
		transports: transports,
	}
}

func (r *NotificationRouter) Notify(ctx context.Context, userID string, notif Notification) {
	// 1. Check user settings
	var settings domain.UserSettings
	if err := r.db.First(&settings, "user_id = ?", userID).Error; err == nil {
		if !settings.NotificationsEnabled {
			// Notifications are globally disabled for this user
			return
		}
	}

	// Check if the user has muted this room
	if notif.RoomID != "" {
		var member domain.RoomMember
		if err := r.db.Where("room_id = ? AND user_id = ?", notif.RoomID, userID).First(&member).Error; err == nil {
			if member.NotificationsMuted {
				// Room notifications are muted
				return
			}
		}
	}

	// 2. Fire BeforeNotify hook (synchronously so plugins can mutate or cancel)
	payload := map[string]interface{}{
		"user_id":      userID,
		"notification": &notif,
	}

	if r.hooks != nil {
		if err := r.hooks.Emit(ctx, plugin.BeforeNotify, payload); err != nil {
			log.Warn().Err(err).Msgf("[NotificationRouter] Notification to user %s was blocked by plugin", userID)
			return
		}

		if modNotifVal, exists := payload["notification"]; exists {
			if modNotif, ok := modNotifVal.(*Notification); ok {
				notif = *modNotif
			} else if modNotifObj, ok := modNotifVal.(Notification); ok {
				notif = modNotifObj
			}
		}
	}

	// 3. Route to transports
	for _, transport := range r.transports {
		go func(t Transport) {
			if err := t.Send(context.Background(), userID, notif); err != nil {
				log.Error().Err(err).Msgf("[NotificationRouter] Transport %s failed to send to user %s", t.Name(), userID)
			}
		}(transport)
	}

	// 4. Fire AfterNotify hook
	if r.hooks != nil {
		r.hooks.EmitAsync(ctx, plugin.AfterNotify, payload)
	}
}
