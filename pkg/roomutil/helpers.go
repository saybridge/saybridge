package roomutil

import (
	"context"
	"errors"

	"github.com/saybridge/saybridge/internal/domain"
)

// EnsureRoomMember validates that a user is a member of the specified room.
// Returns the room and member details if valid.
// This helper eliminates the repeated GetRoomByID + GetRoomMember pattern
// found across message, room, and other use cases.
func EnsureRoomMember(ctx context.Context, roomRepo domain.RoomRepository, roomID, userID string) (*domain.Room, *domain.RoomMember, error) {
	room, err := roomRepo.GetRoomByID(ctx, roomID)
	if err != nil {
		return nil, nil, errors.New("room not found")
	}

	member, err := roomRepo.GetRoomMember(ctx, roomID, userID)
	if err != nil {
		return nil, nil, errors.New("access denied: not a member of this room")
	}

	return room, member, nil
}

// EnsureRoomAdmin validates that a user is an admin of the specified room.
// Returns the room and member details if valid, or error if user is not admin/owner.
func EnsureRoomAdmin(ctx context.Context, roomRepo domain.RoomRepository, roomID, userID string) (*domain.Room, *domain.RoomMember, error) {
	room, member, err := EnsureRoomMember(ctx, roomRepo, roomID, userID)
	if err != nil {
		return nil, nil, err
	}

	if member.RoomRole != "admin" && member.RoomRole != "owner" && member.RoomRole != "moderator" {
		return nil, nil, errors.New("access denied: admin privileges required")
	}

	return room, member, nil
}
