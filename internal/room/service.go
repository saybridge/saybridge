package room

import (
	"context"
	"errors"
	"fmt"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/internal/plugin"
)

type roomUseCase struct {
	repo     domain.RoomRepository
	userRepo domain.UserRepository
	hooks    *plugin.HookRegistry
}

// NewRoomUseCase instantiates a new domain.RoomUseCase business logic service.
func NewRoomUseCase(repo domain.RoomRepository, userRepo domain.UserRepository, hooks *plugin.HookRegistry) domain.RoomUseCase {
	return &roomUseCase{
		repo:     repo,
		userRepo: userRepo,
		hooks:    hooks,
	}
}

func (u *roomUseCase) CreateRoom(ctx context.Context, tenantID, creatorID, name, roomType, description, topic string, isEncrypted bool) (*domain.Room, error) {
	// Validate room type parameters
	if !domain.ValidateRoomType(roomType) {
		return nil, errors.New("invalid room type: must be direct, channel, group, or a registered plugin room type")
	}

	// 1. Direct Room Special Logic
	if roomType == "direct" {
		targetUserID := name
		if targetUserID == "" {
			return nil, errors.New("direct message target user id is required")
		}

		// Verify target user is registered and valid
		_, err := u.userRepo.GetUserByID(ctx, targetUserID)
		if err != nil {
			return nil, errors.New("target user not found")
		}

		// Prevent creating direct room with self
		if creatorID == targetUserID {
			return nil, errors.New("cannot create direct message session with yourself")
		}

		// Return existing direct room if it has already been established
		existing, err := u.repo.GetDirectRoom(ctx, tenantID, creatorID, targetUserID)
		if err == nil {
			return existing, nil
		}

		// Instantiate new direct room (type = "direct", empty room name)
		room := &domain.Room{
			TenantID:    tenantID,
			Type:        "direct",
			IsEncrypted: isEncrypted,
			CreatedBy:   &creatorID,
		}

		// Emit BeforeCreateRoom lifecycle hook for direct room
		if err := u.hooks.Emit(ctx, plugin.BeforeCreateRoom, map[string]interface{}{
			"creator_id": creatorID,
			"name":       targetUserID,
			"room_type":  "direct",
		}); err != nil {
			return nil, err
		}

		if err := u.repo.CreateRoom(ctx, room, creatorID); err != nil {
			return nil, err
		}

		// Add target user as passive participant member
		targetMember := &domain.RoomMember{
			RoomID:   room.ID,
			UserID:   targetUserID,
			RoomRole: "member",
		}
		if err := u.repo.AddRoomMember(ctx, targetMember); err != nil {
			return nil, err
		}

		// Reload room state to bundle both memberships in response
		reloadedRoom, err := u.repo.GetRoomByID(ctx, room.ID)
		if err != nil {
			return nil, err
		}

		// Emit AfterCreateRoom lifecycle hook asynchronously
		u.hooks.EmitAsync(ctx, plugin.AfterCreateRoom, map[string]interface{}{
			"room_id":    reloadedRoom.ID,
			"creator_id": creatorID,
			"name":       targetUserID,
			"room_type":  "direct",
		})

		return reloadedRoom, nil
	}

	// 2. Public Channel or Private Group Logic
	if name == "" {
		return nil, errors.New("room name is required")
	}

	// Emit BeforeCreateRoom lifecycle hook (naming policy, quota limits, etc.)
	if err := u.hooks.Emit(ctx, plugin.BeforeCreateRoom, map[string]interface{}{
		"creator_id": creatorID,
		"name":       name,
		"room_type":  roomType,
	}); err != nil {
		return nil, err
	}

	room := &domain.Room{
		TenantID:    tenantID,
		Name:        name,
		Slug:        u.generateUniqueSlug(ctx, tenantID, name),
		Type:        roomType,
		Description: description,
		Topic:       topic,
		IsEncrypted: isEncrypted,
		CreatedBy:   &creatorID,
	}

	if err := u.repo.CreateRoom(ctx, room, creatorID); err != nil {
		return nil, err
	}

	// Emit AfterCreateRoom lifecycle hook asynchronously (auto-join bot, webhook, etc.)
	u.hooks.EmitAsync(ctx, plugin.AfterCreateRoom, map[string]interface{}{
		"room_id":    room.ID,
		"creator_id": creatorID,
		"name":       name,
		"room_type":  roomType,
	})

	return room, nil
}

func (u *roomUseCase) GetRoomDetails(ctx context.Context, userID, roomID string) (*domain.Room, error) {
	room, err := u.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found")
	}

	// Enforce strict access control: if room is private (group or direct), caller must be an active member
	if room.Type != "channel" {
		_, err = u.repo.GetRoomMember(ctx, roomID, userID)
		if err != nil {
			return nil, errors.New("access denied: you are not a member of this private room")
		}
	}

	return room, nil
}

func (u *roomUseCase) GetRoomBySlug(ctx context.Context, tenantID, slug string) (*domain.Room, error) {
	room, err := u.repo.GetRoomBySlug(ctx, tenantID, slug)
	if err != nil {
		return nil, errors.New("room not found")
	}
	return room, nil
}

func (u *roomUseCase) ListRooms(ctx context.Context, tenantID, userID string) ([]domain.Room, error) {
	return u.repo.ListRoomsForUser(ctx, tenantID, userID)
}

func (u *roomUseCase) InviteMember(ctx context.Context, operatorID, roomID, targetUserID string) (*domain.RoomMember, error) {
	room, err := u.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found")
	}

	// Direct messages do not support manual multi-user invitations
	if room.Type == "direct" {
		return nil, errors.New("cannot invite additional members to direct conversation rooms")
	}

	// Verify inviting operator is member and has role privileges
	opMember, err := u.repo.GetRoomMember(ctx, roomID, operatorID)
	if err != nil {
		return nil, errors.New("access denied: not a member of this room")
	}
	if opMember.RoomRole != "owner" && opMember.RoomRole != "moderator" {
		return nil, errors.New("unauthorized: only owners or moderators can invite new members")
	}

	// Verify target user is active and registered
	_, err = u.userRepo.GetUserByID(ctx, targetUserID)
	if err != nil {
		return nil, errors.New("target user not found")
	}

	// Return current record if target user is already a member
	existing, err := u.repo.GetRoomMember(ctx, roomID, targetUserID)
	if err == nil {
		return existing, nil
	}

	newMember := &domain.RoomMember{
		RoomID:   roomID,
		UserID:   targetUserID,
		RoomRole: "member",
	}

	if err := u.repo.AddRoomMember(ctx, newMember); err != nil {
		return nil, err
	}

	// Emit OnMemberJoin lifecycle hook asynchronously (welcome message, notification, etc.)
	u.hooks.EmitAsync(ctx, plugin.OnMemberJoin, map[string]interface{}{
		"room_id":     roomID,
		"user_id":     targetUserID,
		"operator_id": operatorID,
	})

	return newMember, nil
}

func (u *roomUseCase) KickMember(ctx context.Context, operatorID, roomID, targetUserID string) error {
	if operatorID == targetUserID {
		return errors.New("cannot kick yourself; use leave instead")
	}

	room, err := u.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return errors.New("room not found")
	}

	if room.Type == "direct" {
		return errors.New("cannot kick members from a direct message room")
	}

	// Verify operator privileges
	opMember, err := u.repo.GetRoomMember(ctx, roomID, operatorID)
	if err != nil {
		return errors.New("access denied: not a member of this room")
	}
	if opMember.RoomRole != "owner" && opMember.RoomRole != "moderator" {
		return errors.New("unauthorized: only owners or moderators can kick members")
	}

	// Verify target user membership
	targetMember, err := u.repo.GetRoomMember(ctx, roomID, targetUserID)
	if err != nil {
		return errors.New("target user is not a member of this room")
	}

	// Prevent moderators from kicking owners or moderators
	if opMember.RoomRole == "moderator" && (targetMember.RoomRole == "owner" || targetMember.RoomRole == "moderator") {
		return errors.New("unauthorized: moderators cannot kick owners or fellow moderators")
	}

	if err := u.repo.RemoveRoomMember(ctx, roomID, targetUserID); err != nil {
		return err
	}

	// Emit OnMemberLeave lifecycle hook asynchronously (notification, audit, etc.)
	u.hooks.EmitAsync(ctx, plugin.OnMemberLeave, map[string]interface{}{
		"room_id":     roomID,
		"user_id":     targetUserID,
		"operator_id": operatorID,
	})

	return nil
}

func (u *roomUseCase) LeaveRoom(ctx context.Context, userID, roomID string) error {
	room, err := u.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return errors.New("room not found")
	}

	if room.Type == "direct" {
		return errors.New("cannot leave direct conversation rooms")
	}

	// Verify membership existence
	member, err := u.repo.GetRoomMember(ctx, roomID, userID)
	if err != nil {
		return errors.New("not a member of this room")
	}

	// Check owner constraints to prevent orphan rooms
	if member.RoomRole == "owner" {
		ownerCount := 0
		for _, m := range room.Members {
			if m.RoomRole == "owner" {
				ownerCount++
			}
		}

		if ownerCount == 1 {
			// If sole owner but other members are active, prevent leaving
			if len(room.Members) > 1 {
				return errors.New("cannot leave: you are the sole owner of this room; promote another member to owner first")
			}
		}
	}

	if err := u.repo.RemoveRoomMember(ctx, roomID, userID); err != nil {
		return err
	}

	// Emit OnMemberLeave lifecycle hook asynchronously (notification, audit, etc.)
	u.hooks.EmitAsync(ctx, plugin.OnMemberLeave, map[string]interface{}{
		"room_id":     roomID,
		"user_id":     userID,
		"operator_id": userID,
	})

	return nil
}

func (u *roomUseCase) ToggleE2EE(ctx context.Context, userID, roomID string, enabled bool) (*domain.Room, error) {
	room, err := u.repo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, errors.New("room not found")
	}

	// Only owners and moderators can toggle E2EE
	member, err := u.repo.GetRoomMember(ctx, roomID, userID)
	if err != nil {
		return nil, errors.New("access denied: not a member of this room")
	}
	if member.RoomRole != "owner" && member.RoomRole != "moderator" {
		return nil, errors.New("unauthorized: only owners or moderators can toggle E2EE")
	}

	room.IsEncrypted = enabled
	if err := u.repo.UpdateRoom(ctx, room); err != nil {
		return nil, err
	}

	u.hooks.EmitAsync(ctx, plugin.OnRoomSettingsChanged, map[string]interface{}{
		"room_id":    roomID,
		"user_id":    userID,
		"setting":    "is_encrypted",
		"value":      enabled,
	})

	return room, nil
}

// generateUniqueSlug creates a URL-friendly slug from room name, ensuring uniqueness within tenant.
func (u *roomUseCase) generateUniqueSlug(ctx context.Context, tenantID, name string) string {
	base := domain.Slugify(name)
	slug := base
	for i := 2; i <= 100; i++ {
		_, err := u.repo.GetRoomBySlug(ctx, tenantID, slug)
		if err != nil {
			// Not found = slug is available
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	return slug
}
