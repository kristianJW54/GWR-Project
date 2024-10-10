package main

import (
	"GWR_Project/internal/cache"
	"GWR_Project/internal/service"
	"GWR_Project/pkg/catalogue"
	"context"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"log"
	"os"
	"time"
)

// init is invoked before main()
func init() {
	// loads values from .env into the system
	if err := godotenv.Load(); err != nil {
		log.Print("No .env file found")
	}
}

func main() {

	// Retrieve the API token from the environment variable
	Token := os.Getenv("TRAIN_TOKEN")
	URL := os.Getenv("URL")
	if Token == "" || URL == "" {
		log.Fatal("environment variable is not set")
	}

	stations := catalogue.StationCatalog

	requester := service.NewClientRequester()

	opt := &redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,
	}

	nc, err := cache.NewRedisClient(opt)
	if err != nil {
		log.Fatal(err)
	}

	ps := &service.PollSetup{
		Token:    Token,
		URL:      URL,
		Stations: stations,
	}

	pm := service.NewPollMonitor(10, 5*time.Minute, 30*time.Second)

	pc := &service.PollConfig{
		PollSetup:   ps,
		PollMonitor: pm,
		Requester:   requester,
	}

	ctx, cancel := context.WithCancel(context.Background())

	service.RunPollService(ctx, cancel, pc, nc)

}
