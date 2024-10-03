package service

import (
	"GWR_Project/internal/cache"
	"context"
	"fmt"
	"os"
	"os/signal"
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

	newTicker := time.NewTicker(pm.TickerInterval)
	defer newTicker.Stop()
	pm.WindowStart = time.Now()

	for {
		select {
		case t := <-newTicker.C:
			// Print every tick to verify it's working
			fmt.Println("Ticker fired at: ", t)
			fmt.Println("Request Count: ", pm.RequestCount)
			fmt.Printf("Window Start: %v | Window Limit: %v \n", pm.WindowStart, pm.RequestLimitWindow)

			pm.mu.Lock()

			// Reset window if time since window start exceeds window duration
			if time.Since(pm.WindowStart) > pm.RequestLimitWindow {
				fmt.Println("Window ended, resetting RequestCount")
				pm.RequestCount = 0
				pm.WindowStart = time.Now() // Reset window start time
			}

			// If poll limit is reached, skip polling and continue ticking
			if pm.RequestCount >= pm.PollLimit {
				fmt.Println("Poll limit reached for the current window, skipping poll...")
				pm.mu.Unlock() // Unlock even if skipping
				continue
			}

			// Poll data logic here
			// TODO add the fetch all trains here - monitor time and go routines
			// TODO cache the data - monitor time and go routines
			// TODO check output and success - check time and final go routines
			fmt.Println("Polling data...")
			data, err := FetchAllTrains(token, url, stations, requester)
			if err != nil {
				pm.ErrLog <- err
			}
			err = cache.RedisCacheData(rh, data)
			if err != nil {
				pm.ErrLog <- err
			}
			pm.RequestCount++

			pm.mu.Unlock()

		case <-pm.TickerDone: // If ticker done signal is received, stop the ticker
			fmt.Println("Ticker done")
			return

		case err := <-pm.ErrLog:
			// Handle error received
			fmt.Println("Error in polling data:", err)
			return // Exit the function and prevent further ticker ticks

		case <-ctx.Done(): // If the context is canceled, stop the ticker
			fmt.Println("Context done")
			return
		}
	}

}

func monitorOsSignalShutdown(cancel context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	sig := <-c
	fmt.Println("Got signal: ", sig)
	fmt.Println("Shutting down...")
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

// TODO look at whether we need the PollData wrapper here
// TODO look at what a service level run function will need and begin planning for final service deployment

func RunPollService(ctx context.Context, cancel context.CancelFunc, pc *PollConfig, rh cache.RedisHandler) {

	// TODO add redis client ping check to RedisHandler to check that it's live and ready before beginning poll

	go PollData(ctx, pc.Token, pc.URL, pc.Stations, pc.Requester, pc.PollMonitor, rh)

	// Start error monitoring (runs in the background)
	go MonitorErrors(pc.ErrLog, cancel)

	// Handle OS signal-based graceful shutdown
	go monitorOsSignalShutdown(cancel)

	// Block until the context is canceled (due to OS signal or an error)
	<-ctx.Done()

	fmt.Println("Polling service is shutting down")
	return
}
