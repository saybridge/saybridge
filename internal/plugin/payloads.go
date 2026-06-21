package plugin

import "time"

// ──────────────────────────────────────────────────────────────────────────────
// Typed Hook Payloads — Replace map[string]interface{} with typed structs.
// Each Before* payload supports mutation: handlers can modify fields and the
// core will use the mutated values when persisting (WillBe/HasBeen pattern).
// ──────────────────────────────────────────────────────────────────────────────

// MessagePayload is the typed payload for message lifecycle hooks.
type MessagePayload struct {
	MessageID  string    `json:"message_id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	RoomID     string    `json:"room_id"`
	Content    string    `json:"content"`
	MsgType    string    `json:"msg_type"`
	ParentID   string    `json:"parent_id,omitempty"`
	ReplyToID  string    `json:"reply_to_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`

	// Mutation flag — set by Before* handlers to indicate content was modified
	Mutated bool `json:"-"`
}

// EditMessagePayload is the typed payload for message edit hooks.
type EditMessagePayload struct {
	UserID     string `json:"user_id"`
	RoomID     string `json:"room_id"`
	MessageID  string `json:"message_id"`
	OldContent string `json:"old_content"`
	NewContent string `json:"new_content"`
	Mutated    bool   `json:"-"`
}

// DeleteMessagePayload is the typed payload for message delete hooks.
type DeleteMessagePayload struct {
	UserID    string `json:"user_id"`
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
}

// ReactionPayload is the typed payload for reaction hooks.
type ReactionPayload struct {
	UserID    string `json:"user_id"`
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
	Action    string `json:"action"` // "add" or "remove"
}

// RoomPayload is the typed payload for room lifecycle hooks.
type RoomPayload struct {
	RoomID    string `json:"room_id"`
	CreatorID string `json:"creator_id"`
	Name      string `json:"name"`
	RoomType  string `json:"room_type"`
	Mutated   bool   `json:"-"`
}

// MemberPayload is the typed payload for room member join/leave hooks.
type MemberPayload struct {
	RoomID     string `json:"room_id"`
	UserID     string `json:"user_id"`
	OperatorID string `json:"operator_id"`
}

// AuthPayload is the typed payload for authentication hooks.
type AuthPayload struct {
	UserID     string `json:"user_id"`
	Email      string `json:"email,omitempty"`
	Password   string `json:"-"` // Never serialized
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	IPAddress  string `json:"ip_address,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
}

// RegisterPayload is the typed payload for registration hooks.
type RegisterPayload struct {
	UserID      string `json:"user_id,omitempty"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Mutated     bool   `json:"-"`
}

// NotificationPayload is the typed payload for notification hooks.
type NotificationPayload struct {
	RecipientID string `json:"recipient_id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	RoomID      string `json:"room_id"`
	Mutated     bool   `json:"-"`
}

// SlashCommandPayload is the typed payload for slash command hooks.
type SlashCommandPayload struct {
	Command   string `json:"command"`
	Args      string `json:"args"`
	SenderID  string `json:"sender_id"`
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

// PluginPayload is the typed payload for plugin lifecycle hooks.
type PluginPayload struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ToMap converts a typed payload to map[string]interface{} for backward compatibility
// with existing handlers that use the untyped signature.
func (p *MessagePayload) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"message_id":  p.MessageID,
		"sender_id":   p.SenderID,
		"sender_name": p.SenderName,
		"room_id":     p.RoomID,
		"content":     p.Content,
		"msg_type":    p.MsgType,
		"parent_id":   p.ParentID,
		"reply_to_id": p.ReplyToID,
		"created_at":  p.CreatedAt,
	}
}

// ToMap converts AuthPayload to map for backward compatibility.
func (p *AuthPayload) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"user_id":     p.UserID,
		"email":       p.Email,
		"device_id":   p.DeviceID,
		"device_name": p.DeviceName,
		"ip_address":  p.IPAddress,
		"user_agent":  p.UserAgent,
	}
}
