package server

import (
	"context"
	"fmt"
	"regexp"

	"github.com/saybridge/saybridge/internal/app"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/notification"
	"github.com/saybridge/saybridge/internal/plugin"
)

// RegisterNotificationHook sets up the AfterSendMessage notification dispatch hook.
// When a message is successfully sent, this hook determines recipients and dispatches
// notifications via the notification router.
func RegisterNotificationHook(s *Server, c *app.Container) {
	desktopTransport := notification.NewDesktopTransport(s.natsConn)
	notifRouter := notification.NewNotificationRouter(s.db, s.rdb, plugin.Registry, desktopTransport)

	// Hook: when a message is successfully sent, dispatch notifications
	plugin.Registry.On(plugin.AfterSendMessage, plugin.HookHandler{
		Name:     "core:notifications",
		Priority: 100,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			senderID, _ := payload["sender_id"].(string)
			roomID, _ := payload["room_id"].(string)
			content, _ := payload["content"].(string)
			messageID, _ := payload["message_id"].(string)

			if senderID == "" || roomID == "" {
				return nil, nil
			}

			// Fetch Room details with members
			var room domain.Room
			if err := s.db.Preload("Members").First(&room, "id = ?", roomID).Error; err != nil {
				return nil, err
			}

			// Resolve Sender info
			var sender domain.User
			if err := s.db.First(&sender, "id = ?", senderID).Error; err != nil {
				sender.DisplayName = "System"
			}

			title := fmt.Sprintf("New message from %s", sender.DisplayName)
			if room.Type == "channel" || room.Type == "group" {
				title = fmt.Sprintf("#%s - %s", room.Name, sender.DisplayName)
			}

			notif := notification.Notification{
				Type:   "message",
				Title:  title,
				Body:   content,
				RoomID: roomID,
				Data: map[string]interface{}{
					"message_id":  messageID,
					"sender_id":   senderID,
					"sender_name": sender.DisplayName,
					"room_type":   room.Type,
				},
			}

			// Determine recipients
			recipients := make(map[string]bool)

			if room.Type == "direct" {
				for _, m := range room.Members {
					if m.UserID != senderID {
						recipients[m.UserID] = true
					}
				}
			} else {
				// Parse mentions: e.g. @username
				re := regexp.MustCompile(`@([a-zA-Z0-9_.-]+)`)
				matches := re.FindAllStringSubmatch(content, -1)
				for _, match := range matches {
					username := match[1]
					var user domain.User
					if err := s.db.First(&user, "username = ? AND tenant_id = ?", username, room.TenantID).Error; err == nil {
						// Ensure the mentioned user is actually a member of the room
						isMember := false
						for _, m := range room.Members {
							if m.UserID == user.ID {
								isMember = true
								break
							}
						}
						if isMember && user.ID != senderID {
							recipients[user.ID] = true
						}
					}
				}
			}

			// Dispatch notifications to recipients
			for recipientID := range recipients {
				notifCopy := notif
				if room.Type != "direct" {
					notifCopy.Type = "mention"
				}
				notifRouter.Notify(ctx, recipientID, notifCopy)
			}

			return nil, nil
		},
	})
}
