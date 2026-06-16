// Package stores provides Redis connection initialization.
package stores

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/saybridge/saybridge/pkg/config"
	"github.com/redis/go-redis/v9"
)

// NewConnection initializes a Redis client connection for caching and session storage.
func NewConnection(cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis server: %w", err)
	}

	log.Println("[Infra] Redis client connection established")
	return rdb, nil
}
