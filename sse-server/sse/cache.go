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
	pubsub := rc.Client.PSubscribe(ctx, channel)
	ch := pubsub.Channel() // Get the Go channel for messages
	rc.logger.Info("subscribed on channel", slog.String("channel", channel))

	go func() {
		for msg := range ch {
			select {
			case messageChan <- []byte(msg.Payload): // Forward the Redis message to the SSE message channel
				rc.logger.Info("Received message: ", slog.String("message", msg.Payload))
			case <-ctx.Done():
				rc.logger.Info("Context done, stopping Redis subscriber")
				return
			}
		}
	}()
}
