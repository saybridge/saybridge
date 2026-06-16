package testutil

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────────────
// MockUserRepository — test double for domain.UserRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockUserRepository is a function-injection test double for domain.UserRepository.
// Set any Fn field to override the default zero-value behaviour.
type MockUserRepository struct {
	CreateUserFn         func(ctx context.Context, user *domain.User) error
	GetUserByIDFn        func(ctx context.Context, id string) (*domain.User, error)
	GetUserByEmailFn     func(ctx context.Context, tenantID, email string) (*domain.User, error)
	GetUserByUsernameFn  func(ctx context.Context, tenantID, username string) (*domain.User, error)
	UpdateUserFn         func(ctx context.Context, user *domain.User) error
	CreateTenantFn       func(ctx context.Context, tenant *domain.Tenant) error
	GetTenantByIDFn      func(ctx context.Context, id string) (*domain.Tenant, error)
	GetDefaultTenantFn   func(ctx context.Context) (*domain.Tenant, error)
	UpdateUserSettingsFn func(ctx context.Context, settings *domain.UserSettings) error
	GetUserSettingsFn    func(ctx context.Context, userID string) (*domain.UserSettings, error)
	SearchUsersFn        func(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error)
}

func (m *MockUserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	if m.CreateUserFn != nil {
		return m.CreateUserFn(ctx, user)
	}
	return nil
}

func (m *MockUserRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	if m.GetUserByIDFn != nil {
		return m.GetUserByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *MockUserRepository) GetUserByEmail(ctx context.Context, tenantID, email string) (*domain.User, error) {
	if m.GetUserByEmailFn != nil {
		return m.GetUserByEmailFn(ctx, tenantID, email)
	}
	return nil, nil
}

func (m *MockUserRepository) GetUserByUsername(ctx context.Context, tenantID, username string) (*domain.User, error) {
	if m.GetUserByUsernameFn != nil {
		return m.GetUserByUsernameFn(ctx, tenantID, username)
	}
	return nil, nil
}

func (m *MockUserRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	if m.UpdateUserFn != nil {
		return m.UpdateUserFn(ctx, user)
	}
	return nil
}

func (m *MockUserRepository) CreateTenant(ctx context.Context, tenant *domain.Tenant) error {
	if m.CreateTenantFn != nil {
		return m.CreateTenantFn(ctx, tenant)
	}
	return nil
}

func (m *MockUserRepository) GetTenantByID(ctx context.Context, id string) (*domain.Tenant, error) {
	if m.GetTenantByIDFn != nil {
		return m.GetTenantByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *MockUserRepository) GetDefaultTenant(ctx context.Context) (*domain.Tenant, error) {
	if m.GetDefaultTenantFn != nil {
		return m.GetDefaultTenantFn(ctx)
	}
	return nil, nil
}

func (m *MockUserRepository) UpdateUserSettings(ctx context.Context, settings *domain.UserSettings) error {
	if m.UpdateUserSettingsFn != nil {
		return m.UpdateUserSettingsFn(ctx, settings)
	}
	return nil
}

func (m *MockUserRepository) GetUserSettings(ctx context.Context, userID string) (*domain.UserSettings, error) {
	if m.GetUserSettingsFn != nil {
		return m.GetUserSettingsFn(ctx, userID)
	}
	return nil, nil
}

func (m *MockUserRepository) SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error) {
	if m.SearchUsersFn != nil {
		return m.SearchUsersFn(ctx, tenantID, query, limit)
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MockRoomRepository — test double for domain.RoomRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockRoomRepository is a function-injection test double for domain.RoomRepository.
type MockRoomRepository struct {
	CreateRoomFn          func(ctx context.Context, room *domain.Room, creatorID string) error
	GetRoomByIDFn         func(ctx context.Context, roomID string) (*domain.Room, error)
	GetDirectRoomFn       func(ctx context.Context, tenantID, userA, userB string) (*domain.Room, error)
	ListRoomsForUserFn    func(ctx context.Context, tenantID, userID string) ([]domain.Room, error)
	UpdateRoomFn          func(ctx context.Context, room *domain.Room) error
	AddRoomMemberFn       func(ctx context.Context, member *domain.RoomMember) error
	RemoveRoomMemberFn    func(ctx context.Context, roomID, userID string) error
	GetRoomMemberFn       func(ctx context.Context, roomID, userID string) (*domain.RoomMember, error)
	UpdateRoomMemberFn    func(ctx context.Context, member *domain.RoomMember) error
	SearchRoomsForUserFn  func(ctx context.Context, tenantID, userID, query string, limit int) ([]domain.Room, error)
}

func (m *MockRoomRepository) CreateRoom(ctx context.Context, room *domain.Room, creatorID string) error {
	if m.CreateRoomFn != nil {
		return m.CreateRoomFn(ctx, room, creatorID)
	}
	return nil
}

func (m *MockRoomRepository) GetRoomByID(ctx context.Context, roomID string) (*domain.Room, error) {
	if m.GetRoomByIDFn != nil {
		return m.GetRoomByIDFn(ctx, roomID)
	}
	return nil, nil
}

func (m *MockRoomRepository) GetRoomBySlug(ctx context.Context, tenantID, slug string) (*domain.Room, error) {
	return nil, fmt.Errorf("not found")
}

func (m *MockRoomRepository) GetDirectRoom(ctx context.Context, tenantID, userA, userB string) (*domain.Room, error) {
	if m.GetDirectRoomFn != nil {
		return m.GetDirectRoomFn(ctx, tenantID, userA, userB)
	}
	return nil, nil
}

func (m *MockRoomRepository) ListRoomsForUser(ctx context.Context, tenantID, userID string) ([]domain.Room, error) {
	if m.ListRoomsForUserFn != nil {
		return m.ListRoomsForUserFn(ctx, tenantID, userID)
	}
	return nil, nil
}

func (m *MockRoomRepository) UpdateRoom(ctx context.Context, room *domain.Room) error {
	if m.UpdateRoomFn != nil {
		return m.UpdateRoomFn(ctx, room)
	}
	return nil
}

func (m *MockRoomRepository) AddRoomMember(ctx context.Context, member *domain.RoomMember) error {
	if m.AddRoomMemberFn != nil {
		return m.AddRoomMemberFn(ctx, member)
	}
	return nil
}

func (m *MockRoomRepository) RemoveRoomMember(ctx context.Context, roomID, userID string) error {
	if m.RemoveRoomMemberFn != nil {
		return m.RemoveRoomMemberFn(ctx, roomID, userID)
	}
	return nil
}

func (m *MockRoomRepository) GetRoomMember(ctx context.Context, roomID, userID string) (*domain.RoomMember, error) {
	if m.GetRoomMemberFn != nil {
		return m.GetRoomMemberFn(ctx, roomID, userID)
	}
	return nil, nil
}

func (m *MockRoomRepository) UpdateRoomMember(ctx context.Context, member *domain.RoomMember) error {
	if m.UpdateRoomMemberFn != nil {
		return m.UpdateRoomMemberFn(ctx, member)
	}
	return nil
}

func (m *MockRoomRepository) SearchRoomsForUser(ctx context.Context, tenantID, userID, query string, limit int) ([]domain.Room, error) {
	if m.SearchRoomsForUserFn != nil {
		return m.SearchRoomsForUserFn(ctx, tenantID, userID, query, limit)
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MockMessageRepository — test double for domain.MessageRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockMessageRepository is a function-injection test double for domain.MessageRepository.
type MockMessageRepository struct {
	SaveMessageFn      func(ctx context.Context, msg *domain.Message) error
	GetMessageHistoryFn func(ctx context.Context, roomID string, limit int, beforeID string) ([]domain.Message, error)
	UpdateMessageFn    func(ctx context.Context, msg *domain.Message) error
	GetMessageFn       func(ctx context.Context, roomID string, timeBucket int, messageID string) (*domain.Message, error)
	SaveThreadReplyFn  func(ctx context.Context, msg *domain.Message) error
	GetThreadRepliesFn func(ctx context.Context, parentID string) ([]domain.Message, error)
	GetThreadCountersFn func(ctx context.Context, parentIDs []string) (map[string]int, error)
}

func (m *MockMessageRepository) SaveMessage(ctx context.Context, msg *domain.Message) error {
	if m.SaveMessageFn != nil {
		return m.SaveMessageFn(ctx, msg)
	}
	return nil
}

func (m *MockMessageRepository) GetMessageHistory(ctx context.Context, roomID string, limit int, beforeID string) ([]domain.Message, error) {
	if m.GetMessageHistoryFn != nil {
		return m.GetMessageHistoryFn(ctx, roomID, limit, beforeID)
	}
	return nil, nil
}

func (m *MockMessageRepository) UpdateMessage(ctx context.Context, msg *domain.Message) error {
	if m.UpdateMessageFn != nil {
		return m.UpdateMessageFn(ctx, msg)
	}
	return nil
}

func (m *MockMessageRepository) UpdateMessageContent(ctx context.Context, messageID, content string) error {
	return nil
}

func (m *MockMessageRepository) GetMessage(ctx context.Context, roomID string, timeBucket int, messageID string) (*domain.Message, error) {
	if m.GetMessageFn != nil {
		return m.GetMessageFn(ctx, roomID, timeBucket, messageID)
	}
	return nil, nil
}

func (m *MockMessageRepository) SaveThreadReply(ctx context.Context, msg *domain.Message) error {
	if m.SaveThreadReplyFn != nil {
		return m.SaveThreadReplyFn(ctx, msg)
	}
	return nil
}

func (m *MockMessageRepository) GetThreadReplies(ctx context.Context, parentID string) ([]domain.Message, error) {
	if m.GetThreadRepliesFn != nil {
		return m.GetThreadRepliesFn(ctx, parentID)
	}
	return nil, nil
}

func (m *MockMessageRepository) GetThreadCounters(ctx context.Context, parentIDs []string) (map[string]int, error) {
	if m.GetThreadCountersFn != nil {
		return m.GetThreadCountersFn(ctx, parentIDs)
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MockSearchRepository — test double for domain.SearchRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockSearchRepository is a function-injection test double for domain.SearchRepository.
type MockSearchRepository struct {
	IndexMessageFn  func(ctx context.Context, tenantID string, msg *domain.Message) error
	SearchMessagesFn func(ctx context.Context, tenantID string, roomIDs []string, query string, limit int) ([]domain.Message, error)
}

func (m *MockSearchRepository) IndexMessage(ctx context.Context, tenantID string, msg *domain.Message) error {
	if m.IndexMessageFn != nil {
		return m.IndexMessageFn(ctx, tenantID, msg)
	}
	return nil
}

func (m *MockSearchRepository) SearchMessages(ctx context.Context, tenantID string, roomIDs []string, query string, limit int) ([]domain.Message, error) {
	if m.SearchMessagesFn != nil {
		return m.SearchMessagesFn(ctx, tenantID, roomIDs, query, limit)
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MockStorageRepository — test double for domain.StorageRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockStorageRepository is a function-injection test double for domain.StorageRepository.
type MockStorageRepository struct {
	PresignUploadFn func(ctx context.Context, objectName string, expiry time.Duration) (string, string, error)
	UploadFileFn    func(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error)
	DeleteFileFn    func(ctx context.Context, objectName string) error
	GetFileStreamFn func(ctx context.Context, objectName string) (io.ReadCloser, error)
}

func (m *MockStorageRepository) PresignUpload(ctx context.Context, objectName string, expiry time.Duration) (string, string, error) {
	if m.PresignUploadFn != nil {
		return m.PresignUploadFn(ctx, objectName, expiry)
	}
	return "", "", nil
}

func (m *MockStorageRepository) UploadFile(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) (string, error) {
	if m.UploadFileFn != nil {
		return m.UploadFileFn(ctx, objectName, reader, size, contentType)
	}
	return "", nil
}

func (m *MockStorageRepository) DeleteFile(ctx context.Context, objectName string) error {
	if m.DeleteFileFn != nil {
		return m.DeleteFileFn(ctx, objectName)
	}
	return nil
}

func (m *MockStorageRepository) GetFileStream(ctx context.Context, objectName string) (io.ReadCloser, error) {
	if m.GetFileStreamFn != nil {
		return m.GetFileStreamFn(ctx, objectName)
	}
	return nil, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MockPresenceRepository — test double for domain.PresenceRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockPresenceRepository is a function-injection test double for domain.PresenceRepository.
type MockPresenceRepository struct {
	SetPresenceFn func(ctx context.Context, userID, status string) error
	GetPresenceFn func(ctx context.Context, userID string) (string, error)
}

func (m *MockPresenceRepository) SetPresence(ctx context.Context, userID, status string) error {
	if m.SetPresenceFn != nil {
		return m.SetPresenceFn(ctx, userID, status)
	}
	return nil
}

func (m *MockPresenceRepository) GetPresence(ctx context.Context, userID string) (string, error) {
	if m.GetPresenceFn != nil {
		return m.GetPresenceFn(ctx, userID)
	}
	return "", nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MockReadPositionRepository — test double for domain.ReadPositionRepository
// ──────────────────────────────────────────────────────────────────────────────

// MockReadPositionRepository is a function-injection test double for domain.ReadPositionRepository.
type MockReadPositionRepository struct {
	UpdateReadPositionFn              func(ctx context.Context, rp *domain.ReadPosition) error
	GetReadPositionsFn                func(ctx context.Context, userID string) ([]domain.ReadPosition, error)
	IncrementUnreadForRoomMembersFn   func(ctx context.Context, roomID, senderID string) error
}

func (m *MockReadPositionRepository) UpdateReadPosition(ctx context.Context, rp *domain.ReadPosition) error {
	if m.UpdateReadPositionFn != nil {
		return m.UpdateReadPositionFn(ctx, rp)
	}
	return nil
}

func (m *MockReadPositionRepository) GetReadPositions(ctx context.Context, userID string) ([]domain.ReadPosition, error) {
	if m.GetReadPositionsFn != nil {
		return m.GetReadPositionsFn(ctx, userID)
	}
	return nil, nil
}

func (m *MockReadPositionRepository) IncrementUnreadForRoomMembers(ctx context.Context, roomID, senderID string) error {
	if m.IncrementUnreadForRoomMembersFn != nil {
		return m.IncrementUnreadForRoomMembersFn(ctx, roomID, senderID)
	}
	return nil
}
