package main

import (
	"github.com/joho/godotenv"
	"polling-service/cache"
	"polling-service/pkg/catalogue"
	service2 "polling-service/service"

	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"log/slog"
	"os"
	"time"
)

func init() {
	// Specify the path to the .env file
	if err := godotenv.Load(".env"); err != nil {
		log.Print("godotenv.Load could not find env file - if using docker ignore this error")
	}
}

func pollLogger() *slog.Logger {
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(logHandler)
	return logger
}

func main() {

	// Retrieve the API token and URL from the environment variables
	Token := os.Getenv("TRAIN_TOKEN")
	URL := os.Getenv("URL")
	if Token == "" || URL == "" {
		log.Fatal("environment variable is not set")
	}

	logger := pollLogger()

	stations := catalogue.StationCatalog

	requester := service2.NewClientRequester()

	opt := &redis.Options{
		Addr:     "redis:6379", // Use the service name instead of localhost
		Password: "",           // No password set
		DB:       0,
	}

	time.Sleep(5 * time.Second) // Wait for Redis to start

	nc, err := cache.NewRedisClient(logger, opt)
	if err != nil {
		panic(err)
	}

	ps := &service2.PollSetup{
		Token:    Token,
		URL:      URL,
		Stations: stations,
	}

	pm := service2.NewPollMonitor(5000, 60*time.Minute, 60*time.Second)

	pc := service2.InitPollConfig(logger, ps, pm, requester)

	ctx, cancel := context.WithCancel(context.Background())

	logger.Info("beginning polling of train data")

	service2.RunPollService(ctx, cancel, pc, nc) //Polling go routine service

	time.Sleep(3 * time.Second) // Wait for cleanup
}
