package repositories

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/gorm"
)

type pgFileRepository struct {
	db *gorm.DB
}

// NewPGFileRepository instantiates a GORM-backed domain.FileRepository implementation.
func NewPGFileRepository(db *gorm.DB) domain.FileRepository {
	return &pgFileRepository{db: db}
}

func (r *pgFileRepository) Save(ctx context.Context, file *domain.File) error {
	return r.db.WithContext(ctx).Save(file).Error
}

func (r *pgFileRepository) FindByID(ctx context.Context, id string) (*domain.File, error) {
	var file domain.File
	err := r.db.WithContext(ctx).First(&file, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *pgFileRepository) FindByRoomID(ctx context.Context, tenantID, roomID string, limit, offset int) ([]domain.File, error) {
	var files []domain.File
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND room_id = ? AND status = ?", tenantID, roomID, "completed").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&files).Error
	return files, err
}

func (r *pgFileRepository) FindByUserID(ctx context.Context, tenantID, userID string, limit, offset int) ([]domain.File, error) {
	var files []domain.File
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND status = ?", tenantID, userID, "completed").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&files).Error
	return files, err
}

func (r *pgFileRepository) FindAllTenantFiles(ctx context.Context, tenantID string, limit, offset int) ([]domain.File, error) {
	var files []domain.File
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, "completed").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&files).Error
	return files, err
}

func (r *pgFileRepository) FindSharedFiles(ctx context.Context, tenantID, userID string, limit, offset int) ([]domain.File, error) {
	var files []domain.File
	err := r.db.WithContext(ctx).
		Table("files").
		Joins("JOIN room_members ON room_members.room_id = files.room_id").
		Where("files.tenant_id = ? AND room_members.user_id = ? AND files.user_id != ? AND files.status = ?", tenantID, userID, userID, "completed").
		Order("files.created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&files).Error
	return files, err
}

func (r *pgFileRepository) GetTotalSizeByTenant(ctx context.Context, tenantID string) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&domain.File{}).
		Where("tenant_id = ? AND status = ?", tenantID, "completed").
		Select("COALESCE(SUM(size), 0)").
		Row().
		Scan(&total)
	return total, err
}

func (r *pgFileRepository) UpdateStatus(ctx context.Context, id string, status string) error {
	return r.db.WithContext(ctx).Model(&domain.File{}).
		Where("id = ?", id).
		Update("status", status).Error
}

func (r *pgFileRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.File{}, "id = ?", id).Error
}
