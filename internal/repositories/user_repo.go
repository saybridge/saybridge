package repositories

import (
	"context"
	"errors"

	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/gorm"
)

type pgUserRepository struct {
	db *gorm.DB
}

// NewPGUserRepository instantiates a new GORM-backed domain.UserRepository.
func NewPGUserRepository(db *gorm.DB) domain.UserRepository {
	return &pgUserRepository{db: db}
}

// CreateUser saves a new User and default UserSettings inside an atomic GORM ACID transaction.
func (r *pgUserRepository) CreateUser(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Persist the User record
		if err := tx.Create(user).Error; err != nil {
			return err
		}

		// 2. Initialize and persist standard default UserSettings
		user.Settings.UserID = user.ID
		user.Settings.Language = "en"
		user.Settings.Theme = "dark"
		user.Settings.Timezone = "UTC"
		user.Settings.NotificationsEnabled = true
		user.Settings.NotificationSound = "default"
		user.Settings.DesktopNotifications = true
		user.Settings.MobilePushEnabled = true
		user.Settings.EmailNotifications = "mentions"
		user.Settings.MessagePreviewInPush = true
		user.Settings.AccentColor = "gray"

		if err := tx.Create(&user.Settings).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *pgUserRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Preload("Settings").First(&user, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *pgUserRepository) GetUserByEmail(ctx context.Context, tenantID, email string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Preload("Settings").First(&user, "tenant_id = ? AND email = ?", tenantID, email).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *pgUserRepository) GetUserByUsername(ctx context.Context, tenantID, username string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Preload("Settings").First(&user, "tenant_id = ? AND username = ?", tenantID, username).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *pgUserRepository) UpdateUser(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *pgUserRepository) CreateTenant(ctx context.Context, tenant *domain.Tenant) error {
	return r.db.WithContext(ctx).Create(tenant).Error
}

func (r *pgUserRepository) GetTenantByID(ctx context.Context, id string) (*domain.Tenant, error) {
	var tenant domain.Tenant
	err := r.db.WithContext(ctx).First(&tenant, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &tenant, nil
}

// GetDefaultTenant returns the first Tenant in the database. If empty, it automatically bootstraps a default Tenant.
func (r *pgUserRepository) GetDefaultTenant(ctx context.Context) (*domain.Tenant, error) {
	var tenant domain.Tenant
	err := r.db.WithContext(ctx).First(&tenant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Automatically bootstrap the default workspace tenant
			defaultTenant := domain.Tenant{
				Name:     "Default Workspace",
				Domain:   "localhost",
				Status:   "active",
				MaxUsers: 1000,
			}
			if err := r.db.WithContext(ctx).Create(&defaultTenant).Error; err != nil {
				return nil, err
			}
			return &defaultTenant, nil
		}
		return nil, err
	}
	return &tenant, nil
}

func (r *pgUserRepository) UpdateUserSettings(ctx context.Context, settings *domain.UserSettings) error {
	return r.db.WithContext(ctx).Save(settings).Error
}

func (r *pgUserRepository) GetUserSettings(ctx context.Context, userID string) (*domain.UserSettings, error) {
	var settings domain.UserSettings
	err := r.db.WithContext(ctx).First(&settings, "user_id = ?", userID).Error
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *pgUserRepository) SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error) {
	var users []domain.User
	err := r.db.WithContext(ctx).
		Where("(tenant_id = ? OR (tenant_id = ? AND system_role = 'bot')) AND is_active = true AND (username ILIKE ? OR display_name ILIKE ?)", tenantID, domain.SystemActorID, "%"+query+"%", "%"+query+"%").
		Limit(limit).
		Find(&users).Error
	return users, err
}
