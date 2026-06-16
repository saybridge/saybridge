package message

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// MessageStore is what message service needs from message persistence.
type MessageStore interface {
	SaveMessage(ctx context.Context, msg *domain.Message) error
	GetMessageHistory(ctx context.Context, roomID string, limit int, beforeID string) ([]domain.Message, error)
	UpdateMessage(ctx context.Context, msg *domain.Message) error
	GetMessage(ctx context.Context, roomID string, timeBucket int, messageID string) (*domain.Message, error)
	SaveThreadReply(ctx context.Context, msg *domain.Message) error
	GetThreadReplies(ctx context.Context, parentID string) ([]domain.Message, error)
	GetThreadCounters(ctx context.Context, parentIDs []string) (map[string]int, error)
}

// RoomChecker is what message needs to verify room membership.
type RoomChecker interface {
	GetRoomByID(ctx context.Context, roomID string) (*domain.Room, error)
	GetRoomMember(ctx context.Context, roomID, userID string) (*domain.RoomMember, error)
}

// UserFinder is what message needs to look up users.
type UserFinder interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// ReadPositionStore is what message needs for read tracking.
type ReadPositionStore interface {
	UpdateReadPosition(ctx context.Context, rp *domain.ReadPosition) error
	GetReadPositions(ctx context.Context, userID string) ([]domain.ReadPosition, error)
	IncrementUnreadForRoomMembers(ctx context.Context, roomID, senderID string) error
}

// EventPublisher abstracts event publishing for testability.
type EventPublisher interface {
	PublishRoomEvent(ctx context.Context, tenantID, roomID string, payload interface{}) error
}
