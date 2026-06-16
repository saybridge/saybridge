package domain

import (
	"context"
	"encoding/json"
	"time"
)

// AuditLog records an administrative or security-relevant event for compliance auditing.
type AuditLog struct {
	ID         string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TenantID   string          `gorm:"type:uuid;not null;index" json:"tenant_id"`
	ActorID    string          `gorm:"type:uuid;not null;index" json:"actor_id"`
	ActorName  string          `gorm:"type:varchar(100);not null" json:"actor_name"`
	Action     string          `gorm:"type:varchar(100);not null;index" json:"action"`
	Resource   string          `gorm:"type:varchar(100);not null;index" json:"resource"`
	ResourceID string          `gorm:"type:varchar(255)" json:"resource_id,omitempty"`
	Details    json.RawMessage `gorm:"type:jsonb;default:'{}'" json:"details,omitempty"`
	IPAddress  string          `gorm:"type:varchar(45)" json:"ip_address,omitempty"`
	UserAgent  string          `gorm:"type:text" json:"user_agent,omitempty"`
	CreatedAt  time.Time       `gorm:"index" json:"created_at"`
}

// TableName overrides GORM's default table name.
func (AuditLog) TableName() string { return "audit_logs" }

// AuditLogFilter contains optional filter criteria for listing audit logs.
type AuditLogFilter struct {
	Action     string
	ActorID    string
	Resource   string
	From       *time.Time
	To         *time.Time
	Offset     int
	Limit      int
}

// AuditLogRepository defines the persistence contract for audit log entries.
type AuditLogRepository interface {
	Create(ctx context.Context, log *AuditLog) error
	List(ctx context.Context, tenantID string, filter AuditLogFilter) ([]AuditLog, error)
	Count(ctx context.Context, tenantID string, filter AuditLogFilter) (int64, error)
	Export(ctx context.Context, tenantID string, filter AuditLogFilter) ([]AuditLog, error)
}
