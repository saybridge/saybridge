package domain

import (
	"time"

	"gorm.io/datatypes"
)

// ChatMessage represents a chat message stored in TimescaleDB (PostgreSQL hypertable).
// This replaces the previous ScyllaDB message storage.
type ChatMessage struct {
	ID         string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	RoomID     string         `gorm:"type:uuid;not null" json:"room_id"`
	SenderID   string         `gorm:"type:uuid;not null" json:"sender_id"`
	SenderName string         `gorm:"type:varchar(255);not null" json:"sender_name"`
	Content    string         `gorm:"type:text;not null;default:''" json:"content"`
	MsgType    string         `gorm:"type:varchar(20);not null;default:'text'" json:"msg_type"`
	ParentID   *string        `gorm:"type:uuid;index:idx_chat_messages_parent" json:"parent_id,omitempty"`
	ReplyToID  *string        `gorm:"type:uuid;index:idx_chat_messages_reply_to" json:"reply_to_id,omitempty"`
	IsEdited   bool           `gorm:"default:false" json:"is_edited"`
	IsDeleted  bool           `gorm:"default:false" json:"is_deleted"`
	Reactions  datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"reactions"`
	EditedAt   *time.Time     `json:"edited_at,omitempty"`
	CreatedAt  time.Time      `gorm:"not null;autoCreateTime" json:"created_at"`
}

func (ChatMessage) TableName() string {
	return "chat_messages"
}

// ThreadCounter tracks the reply count for a parent message.
type ThreadCounter struct {
	ParentID   string `gorm:"type:uuid;primaryKey" json:"parent_id"`
	ReplyCount int    `gorm:"not null;default:0" json:"reply_count"`
}

func (ThreadCounter) TableName() string {
	return "thread_counters"
}
