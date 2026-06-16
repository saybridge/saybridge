package stores

import (
	"context"
	"fmt"
	"time"

	"github.com/saybridge/saybridge/internal/domain"
	"github.com/redis/go-redis/v9"
)

type redisPresenceRepository struct {
	rdb *redis.Client
}

// NewRedisPresenceRepository instantiates a Redis-backed domain.PresenceRepository.
func NewRedisPresenceRepository(rdb *redis.Client) domain.PresenceRepository {
	return &redisPresenceRepository{rdb: rdb}
}

func (r *redisPresenceRepository) SetPresence(ctx context.Context, userID, status string) error {
	key := fmt.Sprintf("user:presence:%s", userID)
	// Cache status with a 15-minute expiration
	return r.rdb.Set(ctx, key, status, 15*time.Minute).Err()
}

func (r *redisPresenceRepository) GetPresence(ctx context.Context, userID string) (string, error) {
	key := fmt.Sprintf("user:presence:%s", userID)
	val, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "offline", nil
	} else if err != nil {
		return "offline", err
	}
	return val, nil
}
