package auth

import (
	"context"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
)

// UserStore is what auth needs from user persistence.
type UserStore interface {
	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	GetUserByEmail(ctx context.Context, tenantID, email string) (*domain.User, error)
	UpdateUser(ctx context.Context, user *domain.User) error
	GetDefaultTenant(ctx context.Context) (*domain.Tenant, error)
}

// SessionStore is what auth needs from session persistence.
type SessionStore interface {
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
}
