package sse

import (
	"context"
	"flag"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	EventServer *EventServer
	logger      *slog.Logger
	config      *Config
}

func NewServer(es *EventServer, config *Config, logger *slog.Logger) *Server {
	return &Server{
		EventServer: es,
		logger:      logger,
		config:      config,
	}
}

func ServerHandler(s *Server) http.Handler {

	mux := http.NewServeMux()
	addRoutes(
		mux,
		s.logger,
		s.config,
		s.EventServer)

	var handler http.Handler = mux
	// Can add top level middleware
	return handler
}

// EventServer manages Server-Sent Events (SSE) by handling client connections.
// Each client is represented by a channel of byte slices in the 'clients' map.
// When a client connects a new channel and the connections is added to the map to monitor closures.
// Clients receive messages over their individual channels, and the server uses the HTTP connection
// (via http.ResponseWriter) to push data to them over the established SSE connection.
type EventServer struct {
	context       context.Context
	cancel        context.CancelFunc
	ConnectClient chan chan []byte
	CloseClient   chan chan []byte
	clients       map[chan []byte]struct{} // Map to keep track of connected clients
	sync          sync.Mutex
	subscriber    Subscriber
	logger        *slog.Logger

	srvWg sync.WaitGroup
	reqWg sync.WaitGroup

	//Go-routine tracking fields
	goroutineTracker map[int]string
	trackerMutex     sync.Mutex
	goroutineID      int
}

func NewSSEServer(parentCtx context.Context, sub Subscriber, logger *slog.Logger) *EventServer {
	// Create a cancellable context derived from the parent context
	ctx, cancel := context.WithCancel(parentCtx)

	return &EventServer{
		context:       ctx,
		cancel:        cancel, // Store the cancel function to stop the server later
		ConnectClient: make(chan chan []byte),
		CloseClient:   make(chan chan []byte),
		clients:       make(map[chan []byte]struct{}),
		subscriber:    sub,
		logger:        logger,

		goroutineTracker: make(map[int]string),
		goroutineID:      0,
	}
}

func (sseServer *EventServer) Run() {
	sseServer.logger.Info("Starting server")
	defer sseServer.srvWg.Done()

	for {
		select {
		case <-sseServer.context.Done():
			sseServer.logger.Info("server shutdown context called-- %s", "err", sseServer.context.Err())

			sseServer.reqWg.Wait() // Wait for the request routines to finish

			return

		case clientConnection := <-sseServer.ConnectClient:
			sseServer.sync.Lock()
			sseServer.clients[clientConnection] = struct{}{}
			sseServer.logger.Debug("connecting client", "client", clientConnection)
			log.Printf("connecting client %v", clientConnection)
			sseServer.sync.Unlock()

		case clientDisconnect := <-sseServer.CloseClient:
			sseServer.sync.Lock()
			sseServer.logger.Debug("closing client", "client", sseServer.clients[clientDisconnect])
			log.Printf("closing client %v", sseServer.clients[clientDisconnect])
			delete(sseServer.clients, clientDisconnect)
			sseServer.sync.Unlock()
		}
	}
}

func (sseServer *EventServer) Stop() {
	sseServer.logger.Info("Waiting for server routines to finish")
	for client := range sseServer.clients {
		sseServer.logger.Debug("closing client", "client", client)
		close(client)
		delete(sseServer.clients, client)
	}
	sseServer.srvWg.Wait()
	sseServer.cancel()
}

//================================================
// Shutdown Logic
//================================================

func GracefulShutdown(
	ctx context.Context,
	cancel context.CancelFunc,
	wg *sync.WaitGroup,
	signalChan chan os.Signal,
	logger *slog.Logger,
	srv *http.Server,
) error {
	defer wg.Done() // Ensure WaitGroup is decremented once

	select {
	case <-ctx.Done():
		logger.Info("Context canceled - initiating graceful shutdown")
	case <-signalChan:
		logger.Info("Received shutdown signal - initiating graceful shutdown")
		cancel() // Cancel main context to propagate shutdown
		// Drain remaining signals if any
		for len(signalChan) > 0 {
			<-signalChan
		}
	}

	// Create a shutdown context with a timeout for graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down HTTP server:", "err", err)
		return err
	}

	logger.Info("HTTP server shutdown completed successfully")
	return nil
}

//================================================
// Handling connections
//================================================

func (sseServer *EventServer) HandleConnection(w http.ResponseWriter, req *http.Request) {

	// Set headers to mimic SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Create a combined context that will be canceled when either the request or server context is done
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel() // Ensure that cancel is called when done

	message := make(chan []byte)
	sseServer.ConnectClient <- message

	//keeping the connection alive with keep-alive protocol
	keepAliveTicker := time.NewTicker(15 * time.Second)
	keepAliveMsg := []byte(":keepalive\n\n")

	notify := req.Context().Done()

	sseServer.TrackGoRoutine("request handler routine", func() {
		select {
		case <-notify:
			sseServer.logger.Info("request context done")
			sseServer.CloseClient <- message
			keepAliveTicker.Stop()
		case <-sseServer.context.Done():
			keepAliveTicker.Stop()
			return
		}
	})

	defer func() {
		sseServer.CloseClient <- message
		close(message)
	}()

	clientPathRequested := req.URL.Path
	sseServer.logger.Debug("client path requested", slog.String("path", clientPathRequested))

	sseServer.reqWg.Add(1)
	sseServer.TrackGoRoutine("subscribe routine", func() {
		sseServer.subscriber.Subscribe(ctx, "*", message, &sseServer.reqWg)
	})

	// Loop to handle sending messages or keep-alive signals
	for {
		select {
		case msg, ok := <-message:
			if !ok {
				return
			}
			_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
			if err != nil {
				log.Printf("error writing messeage: %v", err)
				return
			}
			flusher.Flush()
		case <-keepAliveTicker.C:
			// Send the keep-alive ping
			_, err := w.Write(keepAliveMsg)
			if err != nil {
				sseServer.logger.Debug("error writing keepalive", "err", err)
				return
			}
			flusher.Flush()
		case <-notify:
			flusher.Flush()
			return
		case <-sseServer.context.Done():
			return
		}
	}

}

//===================================================================
// Tracking and debugging go-routines for Event server
//===================================================================

func (sseServer *EventServer) TrackGoRoutine(name string, f func()) {

	sseServer.trackerMutex.Lock()
	id := sseServer.goroutineID
	sseServer.goroutineID++
	sseServer.goroutineTracker[id] = name
	sseServer.logger.Debug("started goroutine--", slog.Int("ID", id), slog.String("name", name))
	sseServer.trackerMutex.Unlock()

	go func() {
		defer func() {
			sseServer.trackerMutex.Lock()
			delete(sseServer.goroutineTracker, id)
			sseServer.logger.Debug("finished goroutine--", slog.Int("ID", id), slog.String("name", name))
			sseServer.trackerMutex.Unlock()
		}()
		f()
	}()
}

func (sseServer *EventServer) LogActiveGoRoutines() {
	sseServer.trackerMutex.Lock()
	defer sseServer.trackerMutex.Unlock()
	sseServer.logger.Debug("go routines left in tracker", slog.Int("total", len(sseServer.goroutineTracker)))
	sseServer.logger.Debug("go routines in tracker: ")
	for id, name := range sseServer.goroutineTracker {
		sseServer.logger.Debug("tracking goroutine", slog.String("goroutine", name), slog.Int("ID", id))
	}
}

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

func Run(ctx context.Context, w io.Writer, args []string) error {
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
	config, err := LoadConfig(*configPath)
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
	subscribe, err := NewRedisClient(logger, options)
	if err != nil {
		return err
	}

	// Initialising new SSE Server
	sseServer := NewSSEServer(ctx, subscribe, logger)

	// Initialising new http server
	server := NewServer(sseServer, config, logger)
	// Creating handler for the http server with routes loaded
	srvHandler := ServerHandler(server)

	httpServer := &http.Server{
		Addr:    net.JoinHostPort(config.ServerAddr, config.ServerPort),
		Handler: srvHandler,
	}

	// Go routines for running the server
	sseServer.srvWg.Add(1)
	sseServer.TrackGoRoutine("main-server", func() {
		sseServer.Run()
	})

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
		err = GracefulShutdown(ctx, cancel, &wg, signalChan, logger, httpServer)
		if err != nil {
			logger.Error("GracefulShutdown error", "err", err)
		}
	}()

	wg.Wait()
	logger.Info("Shutdown complete")
	return nil
}
