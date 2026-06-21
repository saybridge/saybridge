package domain

import (
	"context"
	"io"
	"time"

	"github.com/saybridge/saybridge/internal/authz"
)

// UserRepository defines the database persistence contract for Tenant, User, and UserSettings entities.
type UserRepository interface {
	CreateUser(ctx context.Context, user *User) error
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error)
	GetUserByUsername(ctx context.Context, tenantID, username string) (*User, error)
	UpdateUser(ctx context.Context, user *User) error
	CreateTenant(ctx context.Context, tenant *Tenant) error
	GetTenantByID(ctx context.Context, id string) (*Tenant, error)
	GetDefaultTenant(ctx context.Context) (*Tenant, error)
	UpdateUserSettings(ctx context.Context, settings *UserSettings) error
	GetUserSettings(ctx context.Context, userID string) (*UserSettings, error)
	SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]User, error)
}

// TokenPair represents the output of a successful login containing both Access and Refresh Tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // Access token lifetime in seconds
}

// AuthUseCase defines the business use cases for registration, authentication, session refreshing, and signout.
type AuthUseCase interface {
	Register(ctx context.Context, username, email, password, displayName string) (*User, error)
	Login(ctx context.Context, email, password, deviceID, deviceName, ipAddress, userAgent string) (*TokenPair, error)
	Refresh(ctx context.Context, refreshToken, deviceID string) (*TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
}

// UpdateProfileInput holds field pointers for updating a user's profile.
type UpdateProfileInput struct {
	DisplayName     *string
	AvatarURL       *string
	CustomStatus    *string
	CurrentPassword *string
	NewPassword     *string
}

// UserUseCase defines the business use cases for managing user profiles and settings.
type UserUseCase interface {
	GetProfile(ctx context.Context, userID string) (*User, error)
	UpdateProfile(ctx context.Context, userID string, input UpdateProfileInput) (*User, error)
	UpdateSettings(ctx context.Context, userID string, updates map[string]interface{}) (*UserSettings, error)
	SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]User, error)
}

// RoomRepository defines the GORM database persistence contract for chat Rooms and Memberships.
type RoomRepository interface {
	CreateRoom(ctx context.Context, room *Room, creatorID string) error
	GetRoomByID(ctx context.Context, roomID string) (*Room, error)
	GetRoomBySlug(ctx context.Context, tenantID, slug string) (*Room, error)
	GetDirectRoom(ctx context.Context, tenantID, userA, userB string) (*Room, error)
	ListRoomsForUser(ctx context.Context, tenantID, userID string) ([]Room, error)
	UpdateRoom(ctx context.Context, room *Room) error
	AddRoomMember(ctx context.Context, member *RoomMember) error
	RemoveRoomMember(ctx context.Context, roomID, userID string) error
	GetRoomMember(ctx context.Context, roomID, userID string) (*RoomMember, error)
	UpdateRoomMember(ctx context.Context, member *RoomMember) error
	SearchRoomsForUser(ctx context.Context, tenantID, userID, query string, limit int) ([]Room, error)
}

// RoomUseCase defines the business use cases for orchestrating Rooms and Memberships.
type RoomUseCase interface {
	CreateRoom(ctx context.Context, tenantID, creatorID, name, roomType, description, topic string, isEncrypted bool) (*Room, error)
	GetRoomDetails(ctx context.Context, userID, roomID string) (*Room, error)
	GetRoomBySlug(ctx context.Context, tenantID, slug string) (*Room, error)
	ListRooms(ctx context.Context, tenantID, userID string) ([]Room, error)
	InviteMember(ctx context.Context, operatorID, roomID, targetUserID string) (*RoomMember, error)
	KickMember(ctx context.Context, operatorID, roomID, targetUserID string) error
	LeaveRoom(ctx context.Context, userID, roomID string) error
	ToggleE2EE(ctx context.Context, userID, roomID string, enabled bool) (*Room, error)
}

// MessageRepository defines the TimescaleDB persistence contract for chat messages.
type MessageRepository interface {
	SaveMessage(ctx context.Context, msg *Message) error
	GetMessageHistory(ctx context.Context, roomID string, limit int, beforeID string) ([]Message, error)
	UpdateMessage(ctx context.Context, msg *Message) error
	UpdateMessageContent(ctx context.Context, messageID, content string) error
	GetMessage(ctx context.Context, roomID string, timeBucket int, messageID string) (*Message, error)
	// Thread operations
	SaveThreadReply(ctx context.Context, msg *Message) error
	GetThreadReplies(ctx context.Context, parentID string) ([]Message, error)
	GetThreadCounters(ctx context.Context, parentIDs []string) (map[string]int, error)
}

// MessageUseCase defines business operations for real-time messaging services.
type MessageUseCase interface {
	SetEnforcer(enforcer *authz.AuthzEnforcer)
	SendMessage(ctx context.Context, tenantID, senderID, roomID, content, msgType, parentID, replyToID string) (*Message, error)
	GetMessageHistory(ctx context.Context, userID, roomID string, limit int, beforeID string) ([]Message, error)
	EditMessage(ctx context.Context, tenantID, userID, roomID, messageID string, timeBucket int, newContent string) (*Message, error)
	DeleteMessage(ctx context.Context, tenantID, userID, roomID, messageID string, timeBucket int) (*Message, error)
	ToggleReaction(ctx context.Context, tenantID, userID, roomID, messageID string, timeBucket int, emoji string) (*Message, error)
	// Thread operations
	GetThreadReplies(ctx context.Context, userID, roomID, parentID string) ([]Message, error)
}

// SearchRepository defines the search engine interface for full-text search indexing and query.
type SearchRepository interface {
	IndexMessage(ctx context.Context, tenantID string, msg *Message) error
	SearchMessages(ctx context.Context, tenantID string, roomIDs []string, query string, limit int) ([]Message, error)
}

// SearchUseCase orchestrates full-text queries for messages, users, and rooms.
type SearchUseCase interface {
	SearchMessages(ctx context.Context, tenantID, userID, roomID, query string, limit int) ([]Message, error)
	SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]User, error)
	SearchRooms(ctx context.Context, tenantID, userID, query string, limit int) ([]Room, error)
}

// StorageRepository defines interface for pre-signing upload attachments
type StorageRepository interface {
	PresignUpload(ctx context.Context, objectName string, expiry time.Duration) (uploadURL string, downloadURL string, err error)
	UploadFile(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (downloadURL string, err error)
	DeleteFile(ctx context.Context, objectName string) error
	GetFileStream(ctx context.Context, objectName string) (io.ReadCloser, error)
}

// PresenceRepository defines interface for user status cache
type PresenceRepository interface {
	SetPresence(ctx context.Context, userID, status string) error
	GetPresence(ctx context.Context, userID string) (string, error)
}

// ReadPositionRepository defines GORM database contract for read tracking
type ReadPositionRepository interface {
	UpdateReadPosition(ctx context.Context, rp *ReadPosition) error
	GetReadPositions(ctx context.Context, userID string) ([]ReadPosition, error)
	IncrementUnreadForRoomMembers(ctx context.Context, roomID, senderID string) error
}

// FileRepository defines database operations for File metadata.
type FileRepository interface {
	Save(ctx context.Context, file *File) error
	FindByID(ctx context.Context, id string) (*File, error)
	FindByRoomID(ctx context.Context, tenantID, roomID string, limit, offset int) ([]File, error)
	FindByUserID(ctx context.Context, tenantID, userID string, limit, offset int) ([]File, error)
	FindAllTenantFiles(ctx context.Context, tenantID string, limit, offset int) ([]File, error)
	FindSharedFiles(ctx context.Context, tenantID, userID string, limit, offset int) ([]File, error)
	GetTotalSizeByTenant(ctx context.Context, tenantID string) (int64, error)
	UpdateStatus(ctx context.Context, id string, status string) error
	Delete(ctx context.Context, id string) error
}

// FileUseCase orchestrates business logic for file uploads and management.
type FileUseCase interface {
	PresignUpload(ctx context.Context, tenantID, userID, roomID, filename, contentType string, size int64) (uploadURL string, downloadURL string, fileID string, err error)
	UploadFileDirect(ctx context.Context, tenantID, userID, roomID, filename, contentType string, size int64, reader io.Reader) (fileID string, err error)
	ConfirmUpload(ctx context.Context, tenantID, userID, fileID string) error
	ListRoomFiles(ctx context.Context, tenantID, userID, roomID string, limit, offset int) ([]File, error)
	ListUserFiles(ctx context.Context, tenantID, userID string, limit, offset int) ([]File, error)
	ListAllTenantFiles(ctx context.Context, tenantID, userID string, limit, offset int) ([]File, error)
	ListSharedFiles(ctx context.Context, tenantID, userID string, limit, offset int) ([]File, error)
	DeleteFile(ctx context.Context, tenantID, userID, fileID string) error
	GetFileByID(ctx context.Context, tenantID, userID, fileID string) (*File, error)
	GetSenderName(ctx context.Context, userID string) (string, error)
}



