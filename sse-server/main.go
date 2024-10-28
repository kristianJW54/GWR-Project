package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sse-server/sse"
	"sync"
	"syscall"
)

func Logger(level string, addSource bool) *slog.Logger {
	// Parse the log level string
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo // Default to info if invalid level
	}

	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slogLevel,
		AddSource: addSource,
	})
	return slog.New(logHandler)
}

func run(ctx context.Context, w io.Writer, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	flags := flag.NewFlagSet("app", flag.ExitOnError)

	configPath := flags.String("config", "config.yaml", "path to config file")
	logLevel := flags.String("log-level", "info", "Set the log level (debug, info, warn, error)")
	logAddSource := flags.Bool("log-source", false, "Enable source file and line number in logs")

	// Parse the args using the custom FlagSet
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("error parsing flags: %w", err)
	}

	// Check if the file exists
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		return fmt.Errorf("config file does not exist at path: %s", *configPath)
	}

	// Load configuration from file or environment
	config, err := sse.LoadConfig(*configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Apply command-line flags to override config settings if they are set
	finalLogLevel := config.LogLevel
	if *logLevel != "" {
		finalLogLevel = *logLevel
	}
	finalLogAddSource := config.LogAddSource || *logAddSource

	// Initialize the logger based on final log settings
	logger := Logger(finalLogLevel, finalLogAddSource)
	logger.Info("Loaded configuration", "configPath", *configPath)

	//Setup and connect to message service
	options := &redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	}
	subscribe, err := sse.NewRedisClient(logger, options)
	if err != nil {
		return err
	}

	// Initialising new SSE Server
	sseServer := sse.NewSSEServer(ctx, subscribe, logger)

	// Initialising new http server
	server := sse.NewServer(sseServer, config, logger)
	// Creating handler for the http server with routes loaded
	srvHandler := sse.ServerHandler(server)

	httpServer := &http.Server{
		Addr:    net.JoinHostPort(config.ServerAddr, config.ServerPort),
		Handler: srvHandler,
	}

	// Go routines for running the server
	go sseServer.Run()

	go func() {
		logger.Info("listening on...", slog.String("addr", httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil {
			logger.Error("ListendAndServe error", "err", err)
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = sse.GracefulShutdown(ctx, cancel, &wg, signalChan, logger, httpServer)
		if err != nil {
			logger.Error("GracefulShutdown error: ", err)
		}
	}()

	wg.Wait()
	logger.Info("Shutdown complete")
	return nil
}

//=================================================================
// Main application function
//=================================================================

func main() {

	ctx := context.Background()
	if err := run(ctx, os.Stdout, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}
