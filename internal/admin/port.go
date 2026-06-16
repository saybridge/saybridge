package admin

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
)

// AdminStore for admin operations.
type AdminStore interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	UpdateUser(ctx context.Context, user *domain.User) error
}

// AuditStore for audit log operations.
type AuditStore interface {
	CreateLog(ctx context.Context, log *domain.AuditLog) error
	ListLogs(ctx context.Context, tenantID string, limit, offset int) ([]domain.AuditLog, error)
}
