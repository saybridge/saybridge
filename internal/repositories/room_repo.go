package repositories

import (
	"context"

	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/gorm"
)

type pgRoomRepository struct {
	db *gorm.DB
}

// NewPGRoomRepository instantiates a GORM-backed domain.RoomRepository implementation.
func NewPGRoomRepository(db *gorm.DB) domain.RoomRepository {
	return &pgRoomRepository{db: db}
}

func (r *pgRoomRepository) CreateRoom(ctx context.Context, room *domain.Room, creatorID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Persist the Room record
		if err := tx.Create(room).Error; err != nil {
			return err
		}

		// 2. Insert the room creator as initial membership with "owner" role
		member := &domain.RoomMember{
			RoomID:   room.ID,
			UserID:   creatorID,
			RoomRole: "owner",
		}
		if err := tx.Create(member).Error; err != nil {
			return err
		}

		return nil
	})
}

func (r *pgRoomRepository) GetRoomByID(ctx context.Context, roomID string) (*domain.Room, error) {
	var room domain.Room
	err := r.db.WithContext(ctx).Preload("Members").Preload("Members.User").First(&room, "id = ?", roomID).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *pgRoomRepository) GetRoomBySlug(ctx context.Context, tenantID, slug string) (*domain.Room, error) {
	var room domain.Room
	err := r.db.WithContext(ctx).Preload("Members").Preload("Members.User").
		Where("slug = ? AND tenant_id = ?", slug, tenantID).
		First(&room).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *pgRoomRepository) GetDirectRoom(ctx context.Context, tenantID, userA, userB string) (*domain.Room, error) {
	var room domain.Room
	// Perform JOIN on room_members to locate a mutual "direct" room mapping
	err := r.db.WithContext(ctx).
		Table("rooms").
		Joins("JOIN room_members rm1 ON rm1.room_id = rooms.id AND rm1.user_id = ?", userA).
		Joins("JOIN room_members rm2 ON rm2.room_id = rooms.id AND rm2.user_id = ?", userB).
		Where("rooms.type = ? AND rooms.tenant_id = ?", "direct", tenantID).
		Preload("Members").
		Preload("Members.User").
		First(&room).Error
	if err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *pgRoomRepository) ListRoomsForUser(ctx context.Context, tenantID, userID string) ([]domain.Room, error) {
	var rooms []domain.Room
	err := r.db.WithContext(ctx).
		Table("rooms").
		Joins("JOIN room_members rm ON rm.room_id = rooms.id AND rm.user_id = ?", userID).
		Where("rooms.tenant_id = ?", tenantID).
		Preload("Members").
		Preload("Members.User").
		Find(&rooms).Error
	return rooms, err
}

func (r *pgRoomRepository) AddRoomMember(ctx context.Context, member *domain.RoomMember) error {
	return r.db.WithContext(ctx).Create(member).Error
}

func (r *pgRoomRepository) RemoveRoomMember(ctx context.Context, roomID, userID string) error {
	// Execute hard delete on the room_member composite PK record
	return r.db.WithContext(ctx).Delete(&domain.RoomMember{}, "room_id = ? AND user_id = ?", roomID, userID).Error
}

func (r *pgRoomRepository) GetRoomMember(ctx context.Context, roomID, userID string) (*domain.RoomMember, error) {
	var member domain.RoomMember
	err := r.db.WithContext(ctx).First(&member, "room_id = ? AND user_id = ?", roomID, userID).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *pgRoomRepository) UpdateRoomMember(ctx context.Context, member *domain.RoomMember) error {
	return r.db.WithContext(ctx).Save(member).Error
}

func (r *pgRoomRepository) UpdateRoom(ctx context.Context, room *domain.Room) error {
	return r.db.WithContext(ctx).Save(room).Error
}

func (r *pgRoomRepository) SearchRoomsForUser(ctx context.Context, tenantID, userID, query string, limit int) ([]domain.Room, error) {
	var rooms []domain.Room
	err := r.db.WithContext(ctx).
		Table("rooms").
		Joins("JOIN room_members rm ON rm.room_id = rooms.id AND rm.user_id = ?", userID).
		Where("rooms.tenant_id = ? AND rooms.name ILIKE ?", tenantID, "%"+query+"%").
		Preload("Members").
		Limit(limit).
		Find(&rooms).Error
	return rooms, err
}
