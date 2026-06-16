package domain

import (
	"time"
)

// Message represents the structural definition of a chat message in TimescaleDB (PostgreSQL hypertable).
type Message struct {
	RoomID     string    `json:"room_id"`
	TimeBucket int       `json:"time_bucket"`
	MessageID  string    `json:"message_id"` // Stored as a TimeUUID string
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Content    string    `json:"content"`
	MsgType    string    `json:"msg_type"` // 'text', 'image', 'file', 'voice', 'system'
	ParentID    string            `json:"parent_id,omitempty"`
	ThreadCount int               `json:"thread_count,omitempty"`
	IsEdited    bool              `json:"is_edited"`
	IsDeleted   bool              `json:"is_deleted"`
	Reactions   map[string]string `json:"reactions,omitempty"`
	EditedAt    time.Time         `json:"edited_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}
