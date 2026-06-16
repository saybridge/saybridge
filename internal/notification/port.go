package notification

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// UserFinder for looking up sender info.
type UserFinder interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// RoomFinder for room details and members.
type RoomFinder interface {
	GetRoomByID(ctx context.Context, roomID string) (*domain.Room, error)
}

// NotificationTransport for sending notifications.
type NotificationTransport interface {
	Send(ctx context.Context, userID string, notification interface{}) error
}
