package main

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"os"
	"sse-server/sse"
)

func sseLogger() *slog.Logger {
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(logHandler)
	return logger
}

func main() {

	opt := &redis.Options{
		Addr:     "localhost:6379", // Use the service name instead of localhost
		Password: "",               // No password set
		DB:       0,
	}

	logger := sseLogger()

	nc, err := sse.NewRedisClient(logger, opt)
	if err != nil {
		panic(err)
	}

	// Retrieve the number of keys in the current Redis database
	keyCount, err := nc.Client.DBSize(context.Background()).Result()
	if err != nil {
		logger.Error("failed to retrieve the number of keys", slog.String("error", err.Error()))
	} else {
		logger.Info("connected to redis", slog.Int64("key_count", keyCount))
	}

}
