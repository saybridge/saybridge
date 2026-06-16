package repositories

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/gorm"
)

type pgAuditLogRepository struct {
	db *gorm.DB
}

// NewPGAuditLogRepository instantiates a GORM-backed domain.AuditLogRepository implementation.
func NewPGAuditLogRepository(db *gorm.DB) domain.AuditLogRepository {
	return &pgAuditLogRepository{db: db}
}

func (r *pgAuditLogRepository) Create(ctx context.Context, log *domain.AuditLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// applyFilters builds a scoped GORM query from the given AuditLogFilter.
func (r *pgAuditLogRepository) applyFilters(tenantID string, filter domain.AuditLogFilter) *gorm.DB {
	q := r.db.Model(&domain.AuditLog{}).Where("tenant_id = ?", tenantID)

	if filter.Action != "" {
		q = q.Where("action = ?", filter.Action)
	}
	if filter.ActorID != "" {
		q = q.Where("actor_id = ?", filter.ActorID)
	}
	if filter.Resource != "" {
		q = q.Where("resource = ?", filter.Resource)
	}
	if filter.From != nil {
		q = q.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		q = q.Where("created_at <= ?", *filter.To)
	}

	return q
}

func (r *pgAuditLogRepository) List(ctx context.Context, tenantID string, filter domain.AuditLogFilter) ([]domain.AuditLog, error) {
	var logs []domain.AuditLog

	q := r.applyFilters(tenantID, filter).WithContext(ctx).Order("created_at DESC")

	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}

	err := q.Find(&logs).Error
	return logs, err
}

func (r *pgAuditLogRepository) Count(ctx context.Context, tenantID string, filter domain.AuditLogFilter) (int64, error) {
	var count int64
	err := r.applyFilters(tenantID, filter).WithContext(ctx).Count(&count).Error
	return count, err
}

func (r *pgAuditLogRepository) Export(ctx context.Context, tenantID string, filter domain.AuditLogFilter) ([]domain.AuditLog, error) {
	var logs []domain.AuditLog
	// Export returns all matching rows (no pagination) ordered chronologically.
	err := r.applyFilters(tenantID, filter).WithContext(ctx).Order("created_at ASC").Find(&logs).Error
	return logs, err
}
