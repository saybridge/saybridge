package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/crypto"
)

type userUseCase struct {
	repo  domain.UserRepository
	hooks *plugin.HookRegistry
}

// NewUserUseCase instantiates a new domain.UserUseCase business logic service.
func NewUserUseCase(repo domain.UserRepository, hooks *plugin.HookRegistry) domain.UserUseCase {
	return &userUseCase{repo: repo, hooks: hooks}
}

func (u *userUseCase) GetProfile(ctx context.Context, userID string) (*domain.User, error) {
	return u.repo.GetUserByID(ctx, userID)
}

func (u *userUseCase) UpdateProfile(ctx context.Context, userID string, input domain.UpdateProfileInput) (*domain.User, error) {
	user, err := u.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user profile: %w", err)
	}

	if input.DisplayName != nil {
		user.DisplayName = *input.DisplayName
	}
	if input.AvatarURL != nil {
		user.AvatarURL = *input.AvatarURL
	}
	if input.CustomStatus != nil {
		user.CustomStatus = *input.CustomStatus
	}

	if input.CurrentPassword != nil && input.NewPassword != nil && *input.CurrentPassword != "" && *input.NewPassword != "" {
		if !crypto.ComparePassword(*input.CurrentPassword, user.PasswordHash) {
			return nil, errors.New("current password is incorrect")
		}
		if len(*input.NewPassword) < 8 {
			return nil, errors.New("new password must be at least 8 characters")
		}
		hashed, err := crypto.HashPassword(*input.NewPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to secure password: %w", err)
		}
		user.PasswordHash = hashed
	}

	if err := u.repo.UpdateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to update user profile: %w", err)
	}

	// Emit OnProfileUpdate lifecycle hook asynchronously (directory sync, audit, etc.)
	u.hooks.EmitAsync(ctx, plugin.OnProfileUpdate, map[string]interface{}{
		"user_id":       userID,
		"display_name":  user.DisplayName,
		"avatar_url":    user.AvatarURL,
		"custom_status": user.CustomStatus,
	})

	return user, nil
}

func (u *userUseCase) UpdateSettings(ctx context.Context, userID string, updates map[string]interface{}) (*domain.UserSettings, error) {
	settings, err := u.repo.GetUserSettings(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user settings: %w", err)
	}

	// Apply updates to existing settings using reflection-free approach
	if v, ok := updates["language"]; ok {
		settings.Language = v.(string)
	}
	if v, ok := updates["theme"]; ok {
		settings.Theme = v.(string)
	}
	if v, ok := updates["timezone"]; ok {
		settings.Timezone = v.(string)
	}
	if v, ok := updates["notifications_enabled"]; ok {
		settings.NotificationsEnabled = v.(bool)
	}
	if v, ok := updates["desktop_notifications"]; ok {
		settings.DesktopNotifications = v.(bool)
	}
	if v, ok := updates["notification_sound"]; ok {
		settings.NotificationSound = v.(string)
	}
	if v, ok := updates["message_density"]; ok {
		settings.MessageDensity = v.(string)
	}
	if v, ok := updates["font_size"]; ok {
		settings.FontSize = v.(int)
	}
	if v, ok := updates["enter_behavior"]; ok {
		settings.EnterBehavior = v.(string)
	}
	if v, ok := updates["link_preview"]; ok {
		settings.LinkPreview = v.(bool)
	}
	if v, ok := updates["room_sort_order"]; ok {
		settings.RoomSortOrder = v.(string)
	}
	if v, ok := updates["show_read_receipts"]; ok {
		settings.ShowReadReceipts = v.(bool)
	}
	if v, ok := updates["reduce_motion"]; ok {
		settings.ReduceMotion = v.(bool)
	}
	if v, ok := updates["accent_color"]; ok {
		settings.AccentColor = v.(string)
	}
	settings.UpdatedAt = time.Now()

	if err := u.repo.UpdateUserSettings(ctx, settings); err != nil {
		return nil, fmt.Errorf("failed to persist user settings: %w", err)
	}

	return settings, nil
}

func (u *userUseCase) SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return u.repo.SearchUsers(ctx, tenantID, query, limit)
}
