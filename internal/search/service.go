package search

import (
	"context"
	"errors"

	"github.com/saybridge/saybridge/internal/domain"
)

type searchUseCase struct {
	searchRepo domain.SearchRepository
	userRepo   domain.UserRepository
	roomRepo   domain.RoomRepository
}

// NewSearchUseCase instantiates a new domain.SearchUseCase business logic service.
func NewSearchUseCase(
	searchRepo domain.SearchRepository,
	userRepo domain.UserRepository,
	roomRepo domain.RoomRepository,
) domain.SearchUseCase {
	return &searchUseCase{
		searchRepo: searchRepo,
		userRepo:   userRepo,
		roomRepo:   roomRepo,
	}
}

func (u *searchUseCase) SearchMessages(ctx context.Context, tenantID, userID, roomID, query string, limit int) ([]domain.Message, error) {
	if query == "" {
		return []domain.Message{}, nil
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var targetRoomIDs []string

	// If roomID is explicitly requested, enforce security boundary checks
	if roomID != "" {
		_, err := u.roomRepo.GetRoomMember(ctx, roomID, userID)
		if err != nil {
			return nil, errors.New("access denied: not a member of this room")
		}
		targetRoomIDs = []string{roomID}
	} else {
		// Global Search: Find all rooms this user is active in to enforce security boundary
		rooms, err := u.roomRepo.ListRoomsForUser(ctx, tenantID, userID)
		if err != nil {
			return nil, err
		}
		if len(rooms) == 0 {
			return []domain.Message{}, nil
		}
		for _, r := range rooms {
			targetRoomIDs = append(targetRoomIDs, r.ID)
		}
	}

	return u.searchRepo.SearchMessages(ctx, tenantID, targetRoomIDs, query, limit)
}

func (u *searchUseCase) SearchUsers(ctx context.Context, tenantID, query string, limit int) ([]domain.User, error) {
	if query == "" {
		return []domain.User{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return u.userRepo.SearchUsers(ctx, tenantID, query, limit)
}

func (u *searchUseCase) SearchRooms(ctx context.Context, tenantID, userID, query string, limit int) ([]domain.Room, error) {
	if query == "" {
		return []domain.Room{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return u.roomRepo.SearchRoomsForUser(ctx, tenantID, userID, query, limit)
}
