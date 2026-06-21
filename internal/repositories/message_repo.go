package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type timescaleMessageRepository struct {
	db *gorm.DB
}

// NewTimescaleMessageRepository instantiates a new GORM-backed (PostgreSQL/TimescaleDB)
// domain.MessageRepository implementation.
func NewTimescaleMessageRepository(db *gorm.DB) domain.MessageRepository {
	return &timescaleMessageRepository{db: db}
}

func (r *timescaleMessageRepository) SaveMessage(ctx context.Context, msg *domain.Message) error {
	cm := domainToChatMsg(msg)

	if msg.MessageID == "" {
		// Let PostgreSQL generate UUID via gen_random_uuid() default
		cm.ID = ""
	}

	if err := r.db.WithContext(ctx).Create(&cm).Error; err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	msg.MessageID = cm.ID
	msg.CreatedAt = cm.CreatedAt
	return nil
}

func (r *timescaleMessageRepository) GetMessageHistory(ctx context.Context, roomID string, limit int, beforeID string) ([]domain.Message, error) {
	var chatMsgs []domain.ChatMessage

	query := r.db.WithContext(ctx).
		Where("room_id = ?", roomID).
		Order("created_at DESC").
		Limit(limit)

	if beforeID != "" {
		// Look up the cursor message's created_at timestamp
		var cursor domain.ChatMessage
		if err := r.db.WithContext(ctx).Select("created_at").First(&cursor, "id = ?", beforeID).Error; err != nil {
			return nil, fmt.Errorf("cursor message not found: %w", err)
		}
		query = query.Where("created_at < ?", cursor.CreatedAt)
	}

	if err := query.Find(&chatMsgs).Error; err != nil {
		return nil, fmt.Errorf("failed to get message history: %w", err)
	}

	messages := make([]domain.Message, 0, len(chatMsgs))
	for i := range chatMsgs {
		messages = append(messages, chatMsgToDomain(&chatMsgs[i]))
	}

	// Fetch thread counters in batch
	messageIDs := make([]string, 0, len(messages))
	for _, m := range messages {
		messageIDs = append(messageIDs, m.MessageID)
	}
	counters, err := r.GetThreadCounters(ctx, messageIDs)
	if err == nil {
		for i := range messages {
			if count, ok := counters[messages[i].MessageID]; ok {
				messages[i].ThreadCount = count
			}
		}
	}

	return messages, nil
}

func (r *timescaleMessageRepository) UpdateMessage(ctx context.Context, msg *domain.Message) error {
	reactions, _ := json.Marshal(msg.Reactions)

	updates := map[string]interface{}{
		"content":    msg.Content,
		"is_edited":  msg.IsEdited,
		"is_deleted": msg.IsDeleted,
		"reactions":  datatypes.JSON(reactions),
		"edited_at":  msg.EditedAt,
	}

	result := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("id = ?", msg.MessageID).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update message: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("message not found")
	}
	return nil
}

func (r *timescaleMessageRepository) UpdateMessageContent(ctx context.Context, messageID, content string) error {
	result := r.db.WithContext(ctx).
		Model(&domain.ChatMessage{}).
		Where("id = ?", messageID).
		Update("content", content)
	if result.Error != nil {
		return fmt.Errorf("failed to update message content: %w", result.Error)
	}
	return nil
}

func (r *timescaleMessageRepository) GetMessage(ctx context.Context, roomID string, _ int, messageID string) (*domain.Message, error) {
	var cm domain.ChatMessage
	err := r.db.WithContext(ctx).
		Where("id = ? AND room_id = ?", messageID, roomID).
		First(&cm).Error
	if err != nil {
		return nil, fmt.Errorf("message not found: %w", err)
	}

	msg := chatMsgToDomain(&cm)
	counters, err := r.GetThreadCounters(ctx, []string{messageID})
	if err == nil {
		if count, ok := counters[messageID]; ok {
			msg.ThreadCount = count
		}
	}
	return &msg, nil
}

func (r *timescaleMessageRepository) SaveThreadReply(ctx context.Context, msg *domain.Message) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Save the reply message
		cm := domainToChatMsg(msg)
		if msg.MessageID == "" {
			cm.ID = ""
		}

		if err := tx.Create(&cm).Error; err != nil {
			return fmt.Errorf("failed to save thread reply: %w", err)
		}

		msg.MessageID = cm.ID
		msg.CreatedAt = cm.CreatedAt

		// 2. Upsert thread counter
		upsertSQL := `INSERT INTO thread_counters (parent_id, reply_count)
			VALUES (?, 1)
			ON CONFLICT (parent_id) DO UPDATE SET reply_count = thread_counters.reply_count + 1`

		if err := tx.Exec(upsertSQL, msg.ParentID).Error; err != nil {
			return fmt.Errorf("failed to update thread counter: %w", err)
		}

		return nil
	})
}

func (r *timescaleMessageRepository) GetThreadReplies(ctx context.Context, parentID string) ([]domain.Message, error) {
	var chatMsgs []domain.ChatMessage

	err := r.db.WithContext(ctx).
		Where("parent_id = ?", parentID).
		Order("created_at ASC").
		Find(&chatMsgs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get thread replies: %w", err)
	}

	messages := make([]domain.Message, 0, len(chatMsgs))
	for i := range chatMsgs {
		messages = append(messages, chatMsgToDomain(&chatMsgs[i]))
	}
	return messages, nil
}

func (r *timescaleMessageRepository) GetThreadCounters(ctx context.Context, parentIDs []string) (map[string]int, error) {
	counters := make(map[string]int)
	if len(parentIDs) == 0 {
		return counters, nil
	}

	var results []domain.ThreadCounter
	err := r.db.WithContext(ctx).
		Where("parent_id IN ?", parentIDs).
		Find(&results).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get thread counters: %w", err)
	}

	for _, tc := range results {
		counters[tc.ParentID] = tc.ReplyCount
	}
	return counters, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────────

// chatMsgToDomain converts a GORM ChatMessage model to a domain.Message.
func chatMsgToDomain(cm *domain.ChatMessage) domain.Message {
	var reactions map[string]string
	if len(cm.Reactions) > 0 {
		_ = json.Unmarshal(cm.Reactions, &reactions)
	}

	parentID := ""
	if cm.ParentID != nil {
		parentID = *cm.ParentID
	}

	replyToID := ""
	if cm.ReplyToID != nil {
		replyToID = *cm.ReplyToID
	}

	var editedAt time.Time
	if cm.EditedAt != nil {
		editedAt = *cm.EditedAt
	}

	return domain.Message{
		MessageID:  cm.ID,
		RoomID:     cm.RoomID,
		TimeBucket: 0, // not used in TimescaleDB
		SenderID:   cm.SenderID,
		SenderName: cm.SenderName,
		Content:    cm.Content,
		MsgType:    cm.MsgType,
		ParentID:   parentID,
		ReplyToID:  replyToID,
		IsEdited:   cm.IsEdited,
		IsDeleted:  cm.IsDeleted,
		Reactions:  reactions,
		EditedAt:   editedAt,
		CreatedAt:  cm.CreatedAt,
	}
}

// domainToChatMsg converts a domain.Message to a GORM ChatMessage model.
func domainToChatMsg(msg *domain.Message) domain.ChatMessage {
	var parentID *string
	if msg.ParentID != "" {
		parentID = &msg.ParentID
	}

	var replyToID *string
	if msg.ReplyToID != "" {
		replyToID = &msg.ReplyToID
	}

	reactions := datatypes.JSON("{}")
	if len(msg.Reactions) > 0 {
		data, err := json.Marshal(msg.Reactions)
		if err == nil {
			reactions = datatypes.JSON(data)
		}
	}

	var editedAt *time.Time
	if !msg.EditedAt.IsZero() {
		editedAt = &msg.EditedAt
	}

	return domain.ChatMessage{
		ID:         msg.MessageID,
		RoomID:     msg.RoomID,
		SenderID:   msg.SenderID,
		SenderName: msg.SenderName,
		Content:    msg.Content,
		MsgType:    msg.MsgType,
		ParentID:   parentID,
		ReplyToID:  replyToID,
		IsEdited:   msg.IsEdited,
		IsDeleted:  msg.IsDeleted,
		Reactions:  reactions,
		EditedAt:   editedAt,
		CreatedAt:  msg.CreatedAt,
	}
}
