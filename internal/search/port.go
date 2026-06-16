package search

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// SearchStore is what search needs from the search engine.
type SearchStore interface {
	IndexMessage(ctx context.Context, tenantID string, msg *domain.Message) error
	SearchMessages(ctx context.Context, tenantID string, roomIDs []string, query string, limit int) ([]domain.Message, error)
}

// UserFinder for searching users.
type UserFinder interface {
	SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error)
}

// RoomFinder for room-scoped search.
type RoomFinder interface {
	GetRoomMember(ctx context.Context, roomID, userID string) (*domain.RoomMember, error)
	SearchRoomsForUser(ctx context.Context, tenantID, userID, query string, limit int) ([]domain.Room, error)
}
