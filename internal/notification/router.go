package notification

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
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
	Type     string                 `json:"type"` // "message", "mention", "reaction", "invite"
	Title    string                 `json:"title"`
	Body     string                 `json:"body"`
	Priority string                 `json:"priority"` // "low", "normal", "important", "urgent"
	RoomID   string                 `json:"room_id"`
	Data     map[string]interface{} `json:"data"`
}

type NotificationRouter struct {
	db         *gorm.DB
	rdb        *goredis.Client
	hooks      *plugin.HookRegistry
	smart      *SmartFilter
	transports []Transport
}

func NewNotificationRouter(db *gorm.DB, rdb *goredis.Client, hooks *plugin.HookRegistry, transports ...Transport) *NotificationRouter {
	return &NotificationRouter{
		db:         db,
		rdb:        rdb,
		hooks:      hooks,
		smart:      NewSmartFilter(rdb),
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

	// 2. Smart filtering: priority classification, focus-mode suppression, grouping.
	if r.smart != nil {
		senderID, _ := notif.Data["sender_id"].(string)
		senderName, _ := notif.Data["sender_name"].(string)
		roomType, _ := notif.Data["room_type"].(string)

		isVIP := r.smart.IsVIP(ctx, userID, senderID)
		notif.Priority = ClassifyPriority(notif.Body, roomType, userID, senderID, isVIP)

		// Urgent and important notifications always go through untouched. Lower
		// priority traffic is subject to focus mode and grouping.
		if notif.Priority == PriorityNormal || notif.Priority == PriorityLow {
			presence, customStatus := r.recipientStatus(ctx, userID)
			if IsFocusMode(presence, customStatus) {
				log.Debug().Msgf("[NotificationRouter] Suppressed %s notification for user %s (focus mode)", notif.Priority, userID)
				return
			}

			decision := r.smart.Group(ctx, userID, notif.RoomID, senderName)
			if !decision.Deliver {
				log.Debug().Msgf("[NotificationRouter] Grouped notification for user %s in room %s", userID, notif.RoomID)
				return
			}
			notif.Body = decision.Body
		}
	}

	// 3. Fire BeforeNotify hook (synchronously so plugins can mutate or cancel)
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

	// 4. Route to transports
	for _, transport := range r.transports {
		go func(t Transport) {
			if err := t.Send(context.Background(), userID, notif); err != nil {
				log.Error().Err(err).Msgf("[NotificationRouter] Transport %s failed to send to user %s", t.Name(), userID)
			}
		}(transport)
	}

	// 5. Fire AfterNotify hook
	if r.hooks != nil {
		r.hooks.EmitAsync(ctx, plugin.AfterNotify, payload)
	}
}

// recipientStatus resolves the recipient's current presence and custom status for
// focus-mode evaluation. Presence is read from the live Redis cache when
// available, falling back to the persisted value on the user record.
func (r *NotificationRouter) recipientStatus(ctx context.Context, userID string) (presence, customStatus string) {
	var user domain.User
	if err := r.db.Select("presence", "custom_status").First(&user, "id = ?", userID).Error; err == nil {
		presence = user.Presence
		customStatus = user.CustomStatus
	}
	if r.rdb != nil {
		if p, err := r.rdb.Get(ctx, fmt.Sprintf("user:presence:%s", userID)).Result(); err == nil && p != "" {
			presence = p
		}
	}
	return presence, customStatus
}
