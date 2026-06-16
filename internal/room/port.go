package room

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// RoomStore is what room service needs from room persistence.
type RoomStore interface {
	CreateRoom(ctx context.Context, room *domain.Room, creatorID string) error
	GetRoomByID(ctx context.Context, roomID string) (*domain.Room, error)
	GetDirectRoom(ctx context.Context, tenantID, userA, userB string) (*domain.Room, error)
	ListRoomsForUser(ctx context.Context, tenantID, userID string) ([]domain.Room, error)
	UpdateRoom(ctx context.Context, room *domain.Room) error
	AddRoomMember(ctx context.Context, member *domain.RoomMember) error
	RemoveRoomMember(ctx context.Context, roomID, userID string) error
	GetRoomMember(ctx context.Context, roomID, userID string) (*domain.RoomMember, error)
	UpdateRoomMember(ctx context.Context, member *domain.RoomMember) error
	SearchRoomsForUser(ctx context.Context, tenantID, userID, query string, limit int) ([]domain.Room, error)
}

// UserFinder is what room needs to look up users.
type UserFinder interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	GetUserByUsername(ctx context.Context, tenantID, username string) (*domain.User, error)
}
