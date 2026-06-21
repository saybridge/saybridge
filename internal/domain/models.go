package domain

import (
	"time"
)

// BaseModel serves as GORM entity base, defining explicit UUID primary key and JSON tags.
type BaseModel struct {
	ID        string     `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

// Tenant represents multi-tenant data isolation units.
type Tenant struct {
	BaseModel
	Name     string `gorm:"type:varchar(255);not null" json:"name"`
	Domain   string `gorm:"type:varchar(255);uniqueIndex;not null" json:"domain"`
	LogoURL  string `gorm:"type:text" json:"logo_url,omitempty"`
	Status   string `gorm:"type:varchar(20);default:'active'" json:"status"`
	MaxUsers int    `gorm:"type:int;default:1000" json:"max_users"`
}

// User represents chat system User entities.
type User struct {
	BaseModel
	TenantID     string       `gorm:"type:uuid;uniqueIndex:idx_users_tenant_username;uniqueIndex:idx_users_tenant_email;not null" json:"tenant_id"`
	Username     string       `gorm:"type:varchar(50);uniqueIndex:idx_users_tenant_username;not null" json:"username"`
	Email        string       `gorm:"type:varchar(255);uniqueIndex:idx_users_tenant_email;not null" json:"email"`
	PasswordHash string       `gorm:"type:varchar(255)" json:"-"` // Omit password hash in JSON marshalling
	AvatarURL    string       `gorm:"type:text" json:"avatar_url,omitempty"`
	DisplayName  string       `gorm:"type:varchar(100)" json:"display_name,omitempty"`
	SystemRole   string       `gorm:"type:varchar(50);default:'user'" json:"system_role"`
	Presence     string       `gorm:"type:varchar(20);default:'offline'" json:"presence_status"`
	CustomStatus string       `gorm:"type:text" json:"custom_status,omitempty"`
	LastActiveAt *time.Time   `json:"last_active_at,omitempty"`
	IsActive     bool         `gorm:"default:true" json:"is_active"`
	Settings     UserSettings `gorm:"foreignKey:UserID" json:"settings,omitempty"`
}

// UserSettings represents individual configurations for users.
type UserSettings struct {
	UserID               string    `gorm:"type:uuid;primaryKey" json:"user_id"`
	Language             string    `gorm:"type:varchar(10);default:'en'" json:"language"`
	Theme                string    `gorm:"type:varchar(20);default:'dark'" json:"theme"`
	Timezone             string    `gorm:"type:varchar(50);default:'UTC'" json:"timezone"`
	NotificationsEnabled bool      `gorm:"default:true" json:"notifications_enabled"`
	NotificationSound    string    `gorm:"type:varchar(50);default:'default'" json:"notification_sound"`
	DesktopNotifications bool      `gorm:"default:true" json:"desktop_notifications"`
	MobilePushEnabled    bool      `gorm:"default:true" json:"mobile_push_enabled"`
	EmailNotifications   string    `gorm:"type:varchar(20);default:'mentions'" json:"email_notifications"` // 'all', 'mentions', 'none'
	MessagePreviewInPush bool      `gorm:"default:true" json:"message_preview_in_push"`
	// ── Personalization ──
	MessageDensity       string    `gorm:"type:varchar(10);default:'cozy'" json:"message_density"`
	FontSize             int       `gorm:"default:14" json:"font_size"`
	EnterBehavior        string    `gorm:"type:varchar(20);default:'send'" json:"enter_behavior"`
	LinkPreview          bool      `gorm:"default:true" json:"link_preview"`
	RoomSortOrder        string    `gorm:"type:varchar(20);default:'activity'" json:"room_sort_order"`
	ShowReadReceipts     bool      `gorm:"default:true" json:"show_read_receipts"`
	ReduceMotion         bool      `gorm:"default:false" json:"reduce_motion"`
	AccentColor          string    `gorm:"type:varchar(20);default:'gray'" json:"accent_color"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// Session tracks multi-device session tokens and clients.
type Session struct {
	BaseModel
	UserID       string    `gorm:"type:uuid;not null;index" json:"user_id"`
	DeviceID     string    `gorm:"type:varchar(255);not null" json:"device_id"`
	DeviceName   string    `gorm:"type:varchar(100)" json:"device_name,omitempty"`
	IPAddress    string    `gorm:"type:varchar(45)" json:"ip_address,omitempty"` // Supports IPv4 and IPv6
	UserAgent    string    `gorm:"type:text" json:"user_agent,omitempty"`
	LastActiveAt time.Time `gorm:"default:now()" json:"last_active_at"`
}

// Room represents channels, groups, and direct conversation rooms.
type Room struct {
	BaseModel
	TenantID    string       `gorm:"type:uuid;not null;index" json:"tenant_id"`
	Name        string       `gorm:"type:varchar(100)" json:"name,omitempty"`
	Slug        string       `gorm:"type:varchar(120);uniqueIndex:idx_rooms_tenant_slug" json:"slug,omitempty"`
	Type        string       `gorm:"type:varchar(20);not null" json:"type"` // 'direct', 'channel', 'group'
	Description string       `gorm:"type:text" json:"description,omitempty"`
	Topic       string       `gorm:"type:text" json:"topic,omitempty"`
	IsReadOnly  bool         `gorm:"default:false" json:"is_read_only"`
	IsEncrypted bool         `gorm:"default:false" json:"is_encrypted"`
	CreatedBy   *string      `gorm:"type:uuid" json:"created_by,omitempty"`
	Members     []RoomMember `gorm:"foreignKey:RoomID" json:"members,omitempty"`
}

// RoomMember tracks user memberships and moderation roles inside individual rooms.
type RoomMember struct {
	RoomID             string    `gorm:"type:uuid;primaryKey" json:"room_id"`
	UserID             string    `gorm:"type:uuid;primaryKey;index" json:"user_id"`
	User               *User     `gorm:"foreignKey:UserID;references:ID" json:"user,omitempty"`
	RoomRole           string    `gorm:"type:varchar(20);default:'member'" json:"room_role"` // 'owner', 'moderator', 'member'
	NotificationsMuted bool      `gorm:"default:false" json:"notifications_muted"`
	CustomRoomName     string    `gorm:"type:varchar(100)" json:"custom_room_name,omitempty"`
	IsFavorite         bool      `gorm:"default:false" json:"is_favorite"`
	IsBanned           bool      `gorm:"default:false" json:"is_banned"`
	JoinedAt           time.Time `gorm:"default:now()" json:"joined_at"`
}

// ReadPosition tracks the read checkpoint of a user inside a room to count unread messages.
type ReadPosition struct {
	UserID            string    `gorm:"type:uuid;primaryKey" json:"user_id"`
	RoomID            string    `gorm:"type:uuid;primaryKey" json:"room_id"`
	LastReadMessageID string    `gorm:"type:varchar(50);not null" json:"last_read_message_id"`
	UnreadCount       int       `gorm:"default:0" json:"unread_count"`
	HasMention        bool      `gorm:"default:false" json:"has_mention"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// File represents file attachments tracked in database.
type File struct {
	BaseModel
	TenantID    string `gorm:"type:uuid;index;not null" json:"tenant_id"`
	RoomID      *string `gorm:"type:uuid;index" json:"room_id,omitempty"`
	UserID      string `gorm:"type:uuid;index;not null" json:"user_id"`
	StorageKey  string `gorm:"type:varchar(512);not null" json:"storage_key"`
	Filename    string `gorm:"type:varchar(255);not null" json:"filename"`
	Size        int64  `gorm:"type:bigint;not null" json:"size"`
	ContentType string `gorm:"type:varchar(100)" json:"content_type"`
	Status      string `gorm:"type:varchar(20);default:'pending'" json:"status"` // 'pending', 'completed', 'failed'
}

