package presence

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// PresenceStore is what presence needs from cache.
type PresenceStore interface {
	SetPresence(ctx context.Context, userID, status string) error
	GetPresence(ctx context.Context, userID string) (string, error)
}

// UserFinder is what presence needs to look up users.
type UserFinder interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// EventPublisher for broadcasting presence changes.
type EventPublisher interface {
	PublishPresenceEvent(ctx context.Context, tenantID string, payload interface{}) error
}
