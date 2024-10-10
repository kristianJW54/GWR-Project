package service

import (
	"GWR_Project/internal/cache"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"
)

type PollSetup struct {
	Token    string
	URL      string
	Stations []string
}

type PollMonitor struct {
	RequestLimitWindow time.Duration
	PollLimit          int
	ErrLog             chan error
	PollRequestTime    *[]PollRequestTime
	TickerDone         chan bool
	RequestCount       int
	WindowStart        time.Time
	TickerInterval     time.Duration
	mu                 sync.Mutex
}

type PollRequestTime struct {
	PollDuration time.Duration
	TimeStamp    time.Time
}

func NewPollMonitor(limit int, limitWindow time.Duration, tickerInterval time.Duration) *PollMonitor {
	return &PollMonitor{
		RequestLimitWindow: limitWindow,
		PollLimit:          limit,
		ErrLog:             make(chan error, 1),
		PollRequestTime:    &[]PollRequestTime{},
		TickerInterval:     tickerInterval,
		TickerDone:         make(chan bool),
	}
}

func PollData(ctx context.Context, token, url string, stations []string, requester Requester, pm *PollMonitor, rh cache.RedisHandler) {

	//TODO think about metrics for a possible service admin dashboard/cli output maybe channels output the key metrics
	// or store them in memory where another function posts the metrics through internal API
	// or prometheus??

	newTicker := time.NewTicker(pm.TickerInterval)
	defer newTicker.Stop()
	pm.WindowStart = time.Now()

	for {
		select {
		case <-newTicker.C:

			start := time.Now()

			log.Printf("Tick")
			pm.mu.Lock()

			// Reset window if time since window start exceeds window duration
			if time.Since(pm.WindowStart) > pm.RequestLimitWindow {
				pm.RequestCount = 0
				pm.WindowStart = time.Now() // Reset window start time
			}

			// If poll limit is reached, skip polling and continue ticking
			if pm.RequestCount >= pm.PollLimit {
				pm.mu.Unlock() // Unlock even if skipping
				continue
			}

			// Poll data logic here

			log.Printf("Polling data...")
			data, err := FetchAllTrains(token, url, stations, requester) //TODO think about passing context here?
			if err != nil {
				pm.ErrLog <- err
			}
			err = cache.RedisCacheData(rh, data) //TODO think about passing context here? -> or should these child processes have their own Contexts within their structs to maintain?
			if err != nil {
				pm.ErrLog <- err
			}
			pm.RequestCount++

			pm.mu.Unlock()

			endTime := time.Now()
			duration := endTime.Sub(start)
			log.Printf("Completed in %v", duration)

		case <-pm.TickerDone: // If ticker done signal is received, stop the ticker
			log.Printf("Ticker done")
			return

		case err := <-pm.ErrLog:
			// Handle error received
			log.Printf("error in polling data: %s", err)
			return // Exit the function and prevent further ticker ticks

		case <-ctx.Done(): // If the context is canceled, stop the ticker
			log.Printf("context done")
			return
		}
	}

}

func monitorOsSignalShutdown(cancel context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	sig := <-c
	log.Printf("got signal: %v", sig)
	log.Printf("Shutting down...")
	cancel()
}

func MonitorErrors(errChan chan error, cancel context.CancelFunc) {
	for err := range errChan {
		fmt.Println("Error: ", err.Error())
		cancel()
	}
}

type PollConfig struct {
	*PollSetup
	*PollMonitor
	Requester
}

func RunPollService(ctx context.Context, cancel context.CancelFunc, pc *PollConfig, rh cache.RedisHandler) {

	// Redis check
	log.Printf("Pinging Redis...")

	err := rh.PingRedis()
	if err != nil {
		<-ctx.Done()
		return
	}
	log.Printf("Pinging Redis done")
	log.Printf("Beginning Poll")

	log.Printf("Starting number of goroutines: %d", runtime.NumGoroutine())
	go PollData(ctx, pc.Token, pc.URL, pc.Stations, pc.Requester, pc.PollMonitor, rh)

	// Start error monitoring (runs in the background)
	go MonitorErrors(pc.ErrLog, cancel)

	// Handle OS signal-based graceful shutdown
	go monitorOsSignalShutdown(cancel)

	// Block until the context is canceled (due to OS signal or an error)
	<-ctx.Done()

	log.Printf("Polling service is shutting down")
	return
}
