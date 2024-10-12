package service

import (
	"polling-service/cache"

	"context"
	"log/slog"
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

func PollData(ctx context.Context, token, url string, stations []string, requester Requester, pm *PollMonitor, rh cache.RedisHandler, logger *slog.Logger) {

	newTicker := time.NewTicker(pm.TickerInterval)
	logger.Info("new ticker started")
	defer newTicker.Stop()
	pm.WindowStart = time.Now()

	for {

		//TODO May want request scoped context here with the station/id as context Value to pass to the fetch and cache
		// functions in order to track and log the context for the request and control timeouts/cancellations..??

		select {
		case <-newTicker.C:

			start := time.Now()

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

			logger.Info("Polling data...")
			data, err := FetchAllTrains(token, url, stations, requester) //TODO think about passing context here?
			if err != nil {
				pm.ErrLog <- err
			}
			err = cache.RedisCacheData(rh, data, logger) //TODO think about passing context here? -> or should these child processes have their own Contexts within their structs to maintain?
			if err != nil {
				pm.ErrLog <- err
			}
			pm.RequestCount++

			pm.mu.Unlock()

			endTime := time.Now()
			duration := endTime.Sub(start)
			logger.Info("Polling completed", slog.Duration("duration", duration))

		case <-pm.TickerDone: // If ticker done signal is received, stop the ticker
			logger.Info("Ticker done")
			return

		case err := <-pm.ErrLog:
			// Handle error received
			logger.Error("error in polling data", slog.String("error", err.Error()))
			return // Exit the function and prevent further ticker ticks

		case <-ctx.Done(): // If the context is canceled, stop the ticker
			logger.Info("context done")
			return
		}
	}

}

func monitorOsSignalShutdown(cancel context.CancelFunc, logger *slog.Logger) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	sig := <-c
	logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
	logger.Info("Shutting down gracefully...")
	cancel()
}

func MonitorErrors(errChan chan error, cancel context.CancelFunc, logger *slog.Logger) {
	for err := range errChan {
		logger.Error("error on channel received", slog.String("error", err.Error()))
		cancel()
	}
}

type PollConfig struct {
	*PollSetup
	*PollMonitor
	Requester
	logger *slog.Logger
}

func InitPollConfig(logger *slog.Logger, setup *PollSetup, monitor *PollMonitor, requester Requester) *PollConfig {
	pc := &PollConfig{
		setup,
		monitor,
		requester,
		logger,
	}
	return pc
}

func RunPollService(ctx context.Context, cancel context.CancelFunc, pc *PollConfig, rh cache.RedisHandler) {

	pc.logger.Info("Starting number of goroutines: ", slog.Int("goroutines", runtime.NumGoroutine()))

	go PollData(ctx, pc.Token, pc.URL, pc.Stations, pc.Requester, pc.PollMonitor, rh, pc.logger)

	// Start error monitoring (runs in the background)
	go MonitorErrors(pc.ErrLog, cancel, pc.logger)

	// Handle OS signal-based graceful shutdown
	go monitorOsSignalShutdown(cancel, pc.logger)

	// Block until the context is canceled (due to OS signal or an error)
	<-ctx.Done()
	pc.logger.Info("Polling service is shutting down")
	return
}
