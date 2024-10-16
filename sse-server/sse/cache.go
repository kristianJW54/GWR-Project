package sse

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log/slog"
)

type RedisClient struct {
	Client *redis.Client
	subMap map[string]*redis.PubSub
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

//===========================================
// Subscribing logic
//===========================================

type RedisSubscriber interface {
	RedisSubscriber(ctx context.Context, channel string, messageChan chan []byte)
}

func (rc *RedisClient) RedisSubscriber(ctx context.Context, channel string, messageChan chan []byte) {
	// Subscribe to the Redis channel
	pubsub := rc.Client.PSubscribe(ctx, channel)
	ch := pubsub.Channel() // Get the Go channel for messages

	// Listen for messages on the Redis channel and forward them to the SSE messageChan
	go func() {
		for msg := range ch {
			// Forward the Redis message to the SSE message channel
			messageChan <- []byte(msg.Payload)
		}
	}()

	// Handle context cancellation to ensure graceful unsubscription
	<-ctx.Done()

	// Unsubscribe and close the Pub/Sub connection when the context is canceled
	if err := pubsub.Unsubscribe(ctx, channel); err != nil {
		rc.logger.Error("failed to unsubscribe", slog.String("channel", channel), slog.String("error", err.Error()))
	}
	err := pubsub.Close()
	if err != nil {
		return
	} // Ensure the Pub/Sub connection is closed
}
