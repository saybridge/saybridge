package user

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// UserStore is what user service needs.
type UserStore interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUser(ctx context.Context, user *domain.User) error
	UpdateUserSettings(ctx context.Context, settings *domain.UserSettings) error
	GetUserSettings(ctx context.Context, userID string) (*domain.UserSettings, error)
	SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error)
}
