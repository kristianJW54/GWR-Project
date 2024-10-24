package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sse-server/sse"
	"sync"
	"syscall"
	"time"
)

func Logger() *slog.Logger {
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	logger := slog.New(logHandler)
	return logger
}

type DataStream struct {
	input chan string
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

// TODO clean up main function - encapsulate full server lifecycle - context - os.Signals
// TODO make run() function with error int output which will be called by main

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the Mock DataStream
	mc := &DataStream{input: make(chan string)}

	httpLogger := Logger()

	config := &sse.Config{
		ServerAddr: "localhost:",
		ServerPort: "8081",
		AdminToken: "1234",
	}

	// Create the SSE server
	sseServer := sse.NewSSEServer(ctx, mc)

	go sseServer.Run()

	server := sse.NewServer(sseServer, config, httpLogger)
	srvHandler := sse.ServerHandler(server)

	httpServer := &http.Server{
		Addr:    "localhost:" + config.ServerPort,
		Handler: srvHandler,
	}

	go func() {
		log.Printf("Listening on %s\n", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil {
			log.Println("ListenAndServe error:", err)
		}
	}()

	// Set up a signal channel to listen for OS signals
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for cancellation or OS signal to initiate shutdown
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Wait for either context cancellation or an OS signal
		select {
		case <-ctx.Done():
			// Context was canceled
			log.Println("Context canceled, initiating shutdown...")
		case <-signalChan:
			// Received an OS signal
			log.Println("Received shutdown signal, initiating shutdown...")
			cancel() // Cancel the context to signal shutdown
		}

		// Create a shutdown context with a timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		// Shut down the HTTP server
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down HTTP server: %s\n", err)
		}
	}()

	// Wait for all goroutines to finish
	wg.Wait()
	log.Println("Server shutdown complete.")
}
