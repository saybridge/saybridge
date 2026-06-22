package message

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/authz"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/events"
	"github.com/saybridge/saybridge/pkg/metrics"
)

type messageUseCase struct {
	repo        domain.MessageRepository
	userRepo    domain.UserRepository
	roomRepo    domain.RoomRepository
	readPosRepo domain.ReadPositionRepository
	js          nats.JetStreamContext
	hooks       *plugin.HookRegistry
	enforcer    *authz.AuthzEnforcer
}

// NewMessageUseCase instantiates a new domain.MessageUseCase business logic service.
func NewMessageUseCase(
	repo domain.MessageRepository,
	userRepo domain.UserRepository,
	roomRepo domain.RoomRepository,
	readPosRepo domain.ReadPositionRepository,
	js nats.JetStreamContext,
	hooks *plugin.HookRegistry,
) domain.MessageUseCase {
	return &messageUseCase{
		repo:        repo,
		userRepo:    userRepo,
		roomRepo:    roomRepo,
		readPosRepo: readPosRepo,
		js:          js,
		hooks:       hooks,
	}
}

func (u *messageUseCase) SetEnforcer(enforcer *authz.AuthzEnforcer) {
	u.enforcer = enforcer
}

func (u *messageUseCase) checkPermission(ctx context.Context, userID, roomID, action string, ownMessage bool) error {
	if u.enforcer == nil {
		return nil
	}

	user, err := u.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	room, err := u.roomRepo.GetRoomByID(ctx, roomID)
	if err != nil {
		return fmt.Errorf("room not found: %w", err)
	}

	obj := authz.Object{
		Type:       "room",
		ID:         room.ID,
		RoomType:   room.Type,
		IsReadOnly: room.IsReadOnly,
	}
	if room.CreatedBy != nil {
		obj.OwnerID = *room.CreatedBy
	}

	sub := authz.Subject{
		ID:       user.ID,
		Role:     user.SystemRole,
		IsActive: user.IsActive,
	}

	member, err := u.roomRepo.GetRoomMember(ctx, roomID, userID)
	if err == nil && member != nil {
		if member.IsBanned {
			sub.RoomRole = "banned"
			sub.Role = "banned"
		} else {
			sub.RoomRole = member.RoomRole
			if user.SystemRole != "admin" && member.RoomRole != "" {
				sub.Role = member.RoomRole
			}
		}
	} else {
		if user.SystemRole != "admin" {
			sub.Role = "guest"
		}
	}

	effectiveAction := action
	if ownMessage {
		if action == "edit_message" {
			effectiveAction = "edit_own_message"
		} else if action == "delete_message" {
			effectiveAction = "delete_own_message"
		}
	}

	if !u.enforcer.Can(sub, obj, effectiveAction) {
		return fmt.Errorf("permission denied: cannot perform action %s in this room", action)
	}

	return nil
}

func (u *messageUseCase) SendMessage(ctx context.Context, tenantID, senderID, roomID, content, msgType, parentID, replyToID string) (*domain.Message, error) {
	// 1. Verify and retrieve sender identity
	user, err := u.userRepo.GetUserByID(ctx, senderID)
	if err != nil {
		return nil, fmt.Errorf("sender not found: %w", err)
	}

	// 2. Assert destination room exists
	room, err := u.roomRepo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("destination room not found: %w", err)
	}

	// 3. Enforce access authorization boundaries: sender must be an active room member
	_, err = u.roomRepo.GetRoomMember(ctx, roomID, senderID)
	if err != nil {
		return nil, errors.New("access denied: not a member of this room")
	}

	// Casbin permission check
	action := "send_message"
	if parentID != "" {
		action = "reply_message"
	}
	if err := u.checkPermission(ctx, senderID, roomID, action, false); err != nil {
		return nil, err
	}

	// 4. Emit BeforeSendMessage lifecycle hook (profanity filter, spam check, content policy, etc.)
	// If any handler returns error, the message send is halted.
	if err := u.hooks.Emit(ctx, plugin.BeforeSendMessage, map[string]interface{}{
		"sender_id":   senderID,
		"room_id":     roomID,
		"content":     content,
		"msg_type":    msgType,
		"parent_id":   parentID,
		"reply_to_id": replyToID,
	}); err != nil {
		return nil, err
	}

	// 5. Construct domain Message structure
	msg := &domain.Message{
		RoomID:     roomID,
		SenderID:   senderID,
		SenderName: user.DisplayName,
		Content:    content,
		MsgType:    msgType,
		ParentID:   parentID,
		ReplyToID:  replyToID,
		IsEdited:   false,
		IsDeleted:  false,
		CreatedAt:  time.Now(),
	}

	// 6. Persist message record into TimescaleDB
	if parentID != "" {
		if err := u.repo.SaveThreadReply(ctx, msg); err != nil {
			return nil, fmt.Errorf("failed to save thread reply: %w", err)
		}
	} else {
		if err := u.repo.SaveMessage(ctx, msg); err != nil {
			return nil, fmt.Errorf("failed to persist message in history store: %w", err)
		}
		// Increment unread count for other members in PostgreSQL
		if u.readPosRepo != nil {
			_ = u.readPosRepo.IncrementUnreadForRoomMembers(ctx, roomID, senderID)
		}
	}

	metrics.IncMessage(msg.MsgType)

	// 7. Broadcast event payload across all clustered nodes via NATS JetStream Pub/Sub
	subject := events.RoomSubject(tenantID, roomID)
	payload := map[string]interface{}{
		"event":   "msg:receive",
		"room_id": roomID,
		"data":    msg,
	}
	_ = events.PublishJSON(u.js, subject, payload)

	// Prepare recipient IDs
	var recipientIDs []string
	for _, m := range room.Members {
		if m.UserID != senderID {
			recipientIDs = append(recipientIDs, m.UserID)
		}
	}

	// 8. Emit AfterSendMessage lifecycle hook asynchronously (search indexing, push notification, webhook, etc.)
	// Use background context since the HTTP request context may be canceled by the time async handlers run.
	asyncCtx := context.Background()
	u.hooks.EmitAsync(asyncCtx, plugin.AfterSendMessage, map[string]interface{}{
		"sender_id":          senderID,
		"room_id":            roomID,
		"message_id":         msg.MessageID,
		"time_bucket":        msg.TimeBucket,
		"content":            msg.Content,
		"room_type":          room.Type,
		"room_members_count": len(room.Members),
		"recipient_ids":      recipientIDs,
	})

	// 9. Emit MessageSlashCommand lifecycle hook asynchronously for specific slash commands
	if strings.HasPrefix(content, "/") {
		parts := strings.SplitN(content[1:], " ", 2)
		cmd := parts[0]
		args := ""
		if len(parts) > 1 {
			args = parts[1]
		}
		u.hooks.EmitAsync(asyncCtx, plugin.MessageSlashCommand, map[string]interface{}{
			"command":            cmd,
			"args":               args,
			"sender_id":          senderID,
			"room_id":            roomID,
			"message_id":         msg.MessageID,
			"time_bucket":        msg.TimeBucket,
			"content":            msg.Content,
			"room_type":          room.Type,
			"room_members_count": len(room.Members),
			"recipient_ids":      recipientIDs,
		})
	}

	return msg, nil
}

func (u *messageUseCase) GetMessageHistory(ctx context.Context, userID, roomID string, limit int, beforeID string) ([]domain.Message, error) {
	room, err := u.roomRepo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found")
	}

	// Strict access boundary: for private spaces (direct, group), caller must be an active member
	if room.Type != "channel" {
		_, err = u.roomRepo.GetRoomMember(ctx, roomID, userID)
		if err != nil {
			return nil, errors.New("access denied: you are not a member of this private room")
		}
	}

	// Apply query paging bounds
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	return u.repo.GetMessageHistory(ctx, roomID, limit, beforeID)
}

func (u *messageUseCase) EditMessage(ctx context.Context, tenantID, userID, roomID, messageID string, timeBucket int, newContent string) (*domain.Message, error) {
	msg, err := u.repo.GetMessage(ctx, roomID, timeBucket, messageID)
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	if u.enforcer == nil {
		if userID != domain.SystemActorID && msg.SenderID != userID {
			return nil, errors.New("unauthorized: only the author can edit this message")
		}
	} else if userID != domain.SystemActorID {
		ownMessage := msg.SenderID == userID
		if err := u.checkPermission(ctx, userID, roomID, "edit_message", ownMessage); err != nil {
			return nil, err
		}
	}

	msg.Content = newContent
	msg.IsEdited = true
	msg.EditedAt = time.Now()

	// Emit BeforeEditMessage lifecycle hook (content policy re-check, etc.)
	if err := u.hooks.Emit(ctx, plugin.BeforeEditMessage, map[string]interface{}{
		"user_id":     userID,
		"room_id":     roomID,
		"message_id":  messageID,
		"new_content": newContent,
	}); err != nil {
		return nil, err
	}

	if err := u.repo.UpdateMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to save edited message: %w", err)
	}

	asyncCtx := context.Background()
	u.hooks.EmitAsync(asyncCtx, plugin.AfterEditMessage, map[string]interface{}{
		"user_id":     userID,
		"room_id":     roomID,
		"message_id":  messageID,
		"time_bucket": timeBucket,
		"new_content": newContent,
	})

	subject := events.RoomSubject(tenantID, roomID)
	payload := map[string]interface{}{
		"event":   "msg:receive",
		"room_id": roomID,
		"data":    msg,
	}
	_ = events.PublishJSON(u.js, subject, payload)

	return msg, nil
}

func (u *messageUseCase) DeleteMessage(ctx context.Context, tenantID, userID, roomID, messageID string, timeBucket int) (*domain.Message, error) {
	msg, err := u.repo.GetMessage(ctx, roomID, timeBucket, messageID)
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	if u.enforcer == nil {
		isAuthorized := false
		if msg.SenderID == userID {
			isAuthorized = true
		} else {
			member, err := u.roomRepo.GetRoomMember(ctx, roomID, userID)
			if err == nil && (member.RoomRole == "owner" || member.RoomRole == "moderator") {
				isAuthorized = true
			}
		}
		if !isAuthorized {
			return nil, errors.New("unauthorized: insufficient room permissions to delete message")
		}
	} else if userID != domain.SystemActorID {
		ownMessage := msg.SenderID == userID
		if err := u.checkPermission(ctx, userID, roomID, "delete_message", ownMessage); err != nil {
			return nil, err
		}
	}

	// Emit BeforeDeleteMessage lifecycle hook (admin override, compliance hold, etc.)
	if err := u.hooks.Emit(ctx, plugin.BeforeDeleteMessage, map[string]interface{}{
		"user_id":    userID,
		"room_id":    roomID,
		"message_id": messageID,
	}); err != nil {
		return nil, err
	}

	msg.Content = "This message was deleted."
	msg.IsDeleted = true
	msg.IsEdited = true
	msg.EditedAt = time.Now()

	if err := u.repo.UpdateMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to soft-delete message: %w", err)
	}

	asyncCtx := context.Background()
	u.hooks.EmitAsync(asyncCtx, plugin.AfterDeleteMessage, map[string]interface{}{
		"user_id":     userID,
		"room_id":     roomID,
		"message_id":  messageID,
		"time_bucket": timeBucket,
	})

	subject := events.RoomSubject(tenantID, roomID)
	payload := map[string]interface{}{
		"event":   "msg:receive",
		"room_id": roomID,
		"data":    msg,
	}
	_ = events.PublishJSON(u.js, subject, payload)

	return msg, nil
}

func (u *messageUseCase) ToggleReaction(ctx context.Context, tenantID, userID, roomID, messageID string, timeBucket int, emoji string) (*domain.Message, error) {
	msg, err := u.repo.GetMessage(ctx, roomID, timeBucket, messageID)
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	_, err = u.roomRepo.GetRoomMember(ctx, roomID, userID)
	if err != nil {
		return nil, errors.New("access denied: not a member of this room")
	}

	if userID != domain.SystemActorID {
		if err := u.checkPermission(ctx, userID, roomID, "react_message", false); err != nil {
			return nil, err
		}
	}

	if msg.Reactions == nil {
		msg.Reactions = make(map[string]string)
	}

	var users []string
	if val, ok := msg.Reactions[emoji]; ok && val != "" {
		_ = json.Unmarshal([]byte(val), &users)
	}

	foundIndex := -1
	for idx, uID := range users {
		if uID == userID {
			foundIndex = idx
			break
		}
	}

	if foundIndex != -1 {
		users = append(users[:foundIndex], users[foundIndex+1:]...)
	} else {
		users = append(users, userID)
	}

	if len(users) == 0 {
		delete(msg.Reactions, emoji)
	} else {
		usersJSON, _ := json.Marshal(users)
		msg.Reactions[emoji] = string(usersJSON)
	}

	if err := u.repo.UpdateMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to toggle message reaction: %w", err)
	}

	subject := events.RoomSubject(tenantID, roomID)
	payload := map[string]interface{}{
		"event":   "msg:receive",
		"room_id": roomID,
		"data":    msg,
	}
	_ = events.PublishJSON(u.js, subject, payload)

	return msg, nil
}

func (u *messageUseCase) GetThreadReplies(ctx context.Context, userID, roomID, parentID string) ([]domain.Message, error) {
	_, err := u.roomRepo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found")
	}

	// Strict access boundary: caller must be a room member
	_, err = u.roomRepo.GetRoomMember(ctx, roomID, userID)
	if err != nil {
		return nil, errors.New("access denied: not a member of this room")
	}

	return u.repo.GetThreadReplies(ctx, parentID)
}
