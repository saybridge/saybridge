//go:build !tinygo

package slashcmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"gorm.io/gorm"
)

func init() {
	plugin.Registry.On(plugin.OnServerStart, plugin.HookHandler{
		Name:     "slashcmd:init",
		Priority: 40,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			db, _ := payload["db"].(*gorm.DB)
			editMessageFn, _ := payload["edit_message_fn"].(func(ctx context.Context, userID, roomID, messageID, newContent string) error)

			// Reconstruct a minimal Dependencies with only the fields slashcmd uses
			deps := plugin.Dependencies{
				DB:            db,
				Hooks:         plugin.Registry,
				EditMessageFn: editMessageFn,
			}

			register(deps)
			return nil, nil
		},
	})
}

// register contains the original Register() logic.
func register(deps plugin.Dependencies) {
	deps.Hooks.On(plugin.MessageSlashCommand, plugin.HookHandler{
		Name:     "slashcmd:router",
		Priority: 10,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			command, _ := payload["command"].(string)
			args, _ := payload["args"].(string)
			senderID, _ := payload["sender_id"].(string)
			roomID, _ := payload["room_id"].(string)
			messageID, _ := payload["message_id"].(string)

			args = strings.TrimSpace(args)

			var sender domain.User
			if err := deps.DB.First(&sender, "id = ?", senderID).Error; err != nil {
				return nil, err
			}

			switch command {
			case "ban":
				return handleBan(ctx, deps, roomID, messageID, args)
			case "kick":
				return handleKick(ctx, deps, roomID, messageID, args)
			case "mute":
				return handleMute(ctx, deps, roomID, senderID, messageID)
			case "leave":
				return handleLeave(ctx, deps, roomID, senderID, messageID, sender.Username)
			case "topic":
				return handleTopic(ctx, deps, roomID, messageID, args)
			case "status":
				return handleStatus(ctx, deps, roomID, senderID, messageID, args)
			case "me":
				return handleMe(ctx, deps, roomID, messageID, sender.Username, args)
			case "shrug":
				return handleShrug(ctx, deps, roomID, messageID, args)
			case "lenny":
				return handleLenny(ctx, deps, roomID, messageID, args)
			case "help":
				return handleHelp(ctx, deps, roomID, messageID)
			}

			return nil, nil
		},
	})
}

func handleBan(ctx context.Context, deps plugin.Dependencies, roomID, messageID, args string) (interface{}, error) {
	if args == "" {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, "❌ Usage: `/ban <username>`")
		return nil, nil
	}

	var targetUser domain.User
	if err := deps.DB.First(&targetUser, "username = ?", args).Error; err != nil {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("❌ User `%s` not found", args))
		return nil, nil
	}

	var member domain.RoomMember
	if err := deps.DB.First(&member, "room_id = ? AND user_id = ?", roomID, targetUser.ID).Error; err != nil {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, "❌ User is not a member of this room")
		return nil, nil
	}

	member.IsBanned = true
	if err := deps.DB.Save(&member).Error; err != nil {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("❌ Ban failed: %v", err))
		return nil, nil
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("🚫 **@%s** has been banned", targetUser.Username))
	return nil, nil
}

func handleKick(ctx context.Context, deps plugin.Dependencies, roomID, messageID, args string) (interface{}, error) {
	if args == "" {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, "❌ Usage: `/kick <username>`")
		return nil, nil
	}

	var targetUser domain.User
	if err := deps.DB.First(&targetUser, "username = ?", args).Error; err != nil {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("❌ User `%s` not found", args))
		return nil, nil
	}

	var member domain.RoomMember
	if err := deps.DB.First(&member, "room_id = ? AND user_id = ?", roomID, targetUser.ID).Error; err != nil {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, "❌ User is not a member of this room")
		return nil, nil
	}

	if err := deps.DB.Delete(&member).Error; err != nil {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("❌ Kick failed: %v", err))
		return nil, nil
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("🥾 **@%s** has been kicked", targetUser.Username))
	return nil, nil
}

func handleMute(ctx context.Context, deps plugin.Dependencies, roomID, senderID, messageID string) (interface{}, error) {
	var member domain.RoomMember
	if err := deps.DB.First(&member, "room_id = ? AND user_id = ?", roomID, senderID).Error; err != nil {
		return nil, err
	}

	member.NotificationsMuted = !member.NotificationsMuted
	if err := deps.DB.Save(&member).Error; err != nil {
		return nil, err
	}

	statusStr := "muted"
	if !member.NotificationsMuted {
		statusStr = "unmuted"
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("🔔 Notifications %s for this room", statusStr))
	return nil, nil
}

func handleLeave(ctx context.Context, deps plugin.Dependencies, roomID, senderID, messageID, username string) (interface{}, error) {
	var member domain.RoomMember
	if err := deps.DB.First(&member, "room_id = ? AND user_id = ?", roomID, senderID).Error; err != nil {
		return nil, err
	}

	if err := deps.DB.Delete(&member).Error; err != nil {
		return nil, err
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("🚪 **@%s** left the room", username))
	return nil, nil
}

func handleTopic(ctx context.Context, deps plugin.Dependencies, roomID, messageID, args string) (interface{}, error) {
	if args == "" {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, "❌ Usage: `/topic <new room topic>`")
		return nil, nil
	}

	if err := deps.DB.Model(&domain.Room{}).Where("id = ?", roomID).Update("topic", args).Error; err != nil {
		return nil, err
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("📢 Room topic updated to: *%s*", args))
	return nil, nil
}

func handleStatus(ctx context.Context, deps plugin.Dependencies, roomID, senderID, messageID, args string) (interface{}, error) {
	if err := deps.DB.Model(&domain.User{}).Where("id = ?", senderID).Update("custom_status", args).Error; err != nil {
		return nil, err
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("💬 Status updated to: *%s*", args))
	return nil, nil
}

func handleMe(ctx context.Context, deps plugin.Dependencies, roomID, messageID, username, args string) (interface{}, error) {
	if args == "" {
		_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, "❌ Usage: `/me <action>`")
		return nil, nil
	}

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, fmt.Sprintf("_@%s %s_", username, args))
	return nil, nil
}

func handleShrug(ctx context.Context, deps plugin.Dependencies, roomID, messageID, args string) (interface{}, error) {
	content := "¯\\_(ツ)_/¯"
	if args != "" {
		content = args + " ¯\\_(ツ)_/¯"
	}
	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, content)
	return nil, nil
}

func handleLenny(ctx context.Context, deps plugin.Dependencies, roomID, messageID, args string) (interface{}, error) {
	content := "( ͡° ͜ʖ ͡°)"
	if args != "" {
		content = args + " ( ͡° ͜ʖ ͡°)"
	}
	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, content)
	return nil, nil
}

func handleHelp(ctx context.Context, deps plugin.Dependencies, roomID, messageID string) (interface{}, error) {
	helpText := `💡 **Available slash commands:**
• ` + "`" + `/ban <username>` + "`" + ` - Ban user from room
• ` + "`" + `/kick <username>` + "`" + ` - Kick user from room
• ` + "`" + `/mute` + "`" + ` - Toggle room notifications mute
• ` + "`" + `/leave` + "`" + ` - Leave the room
• ` + "`" + `/topic <topic>` + "`" + ` - Update room topic
• ` + "`" + `/status <status>` + "`" + ` - Update custom status
• ` + "`" + `/me <action>` + "`" + ` - Send action message
• ` + "`" + `/shrug <text>` + "`" + ` - Appends ¯\_(ツ)_/¯
• ` + "`" + `/lenny <text>` + "`" + ` - Appends ( ͡° ͜ʖ ͡°)
• ` + "`" + `/help` + "`" + ` - Show this list`

	_ = deps.EditMessageFn(ctx, domain.SystemActorID, roomID, messageID, helpText)
	return nil, nil
}
