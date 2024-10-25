package sse

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"log"
	"log/slog"
	"time"
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

//===========================================
// Subscribing logic
//===========================================

type Subscriber interface {
	Subscribe(ctx context.Context, channel string, messageChan chan []byte)
}

func (rc *RedisClient) Subscribe(ctx context.Context, channel string, messageChan chan []byte) {
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

//========================================================
// Mock Data Stream For Testing and Running with CLI Flags
//========================================================

type DataStream struct {
	Input chan string
}

func (ds *DataStream) Subscribe(ctx context.Context, channel string, messageChan chan []byte) {
	go func() {
		// Send 5 messages to simulate Redis messages
		for i := 0; i < 5; i++ {
			select {
			case <-ctx.Done():
				log.Println("Context done, stopping message generation")
				return
			default:
				// Create a test message and send it to the message channel
				message := fmt.Sprintf("Test message %d", i)
				messageChan <- []byte(message) // Send to SSE's message channel
				time.Sleep(1 * time.Second)    // Simulate delay between messages
			}
		}

	}()
}
