package sse

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log/slog"
)

type RedisClient struct {
	Client *redis.Client
	logger *slog.Logger
}

func NewRedisClient(logger *slog.Logger, options *redis.Options) (*RedisClient, error) {
	rc := &RedisClient{
		Client: redis.NewClient(options),
		logger: logger,
	}
	logger.Info("connecting to redis...")

	// Attempting Redis connection, log any potential error
	_, err := rc.Client.Ping(context.Background()).Result()
	if err != nil {
		logger.Error("failed to connect to Redis", slog.String("error", err.Error()))
		return nil, err
	}

	logger.Info("successfully connected to Redis")
	return rc, nil
}
