package repositories

import (
	"context"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type pgReadPositionRepository struct {
	db *gorm.DB
}

// NewPGReadPositionRepository instantiates a GORM-backed domain.ReadPositionRepository.
func NewPGReadPositionRepository(db *gorm.DB) domain.ReadPositionRepository {
	return &pgReadPositionRepository{db: db}
}

func (r *pgReadPositionRepository) UpdateReadPosition(ctx context.Context, rp *domain.ReadPosition) error {
	rp.UpdatedAt = time.Now()
	rp.UnreadCount = 0
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "room_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_read_message_id", "unread_count", "updated_at"}),
	}).Create(rp).Error
}

func (r *pgReadPositionRepository) GetReadPositions(ctx context.Context, userID string) ([]domain.ReadPosition, error) {
	var rps []domain.ReadPosition
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rps).Error
	return rps, err
}

func (r *pgReadPositionRepository) IncrementUnreadForRoomMembers(ctx context.Context, roomID, senderID string) error {
	// Find active room members who are not the sender
	var members []domain.RoomMember
	err := r.db.WithContext(ctx).Where("room_id = ? AND user_id != ?", roomID, senderID).Find(&members).Error
	if err != nil {
		return err
	}

	now := time.Now()
	for _, m := range members {
		// Increment or insert if not exists
		err = r.db.WithContext(ctx).Exec(`
			INSERT INTO read_positions (user_id, room_id, last_read_message_id, unread_count, updated_at)
			VALUES (?, ?, '', 1, ?)
			ON CONFLICT (user_id, room_id)
			DO UPDATE SET unread_count = read_positions.unread_count + 1, updated_at = ?`,
			m.UserID, roomID, now, now).Error
		if err != nil {
			// Skip single record failure to prevent blocking the message lifecycle
			continue
		}
	}
	return nil
}
