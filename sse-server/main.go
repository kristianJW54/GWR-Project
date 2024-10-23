package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sse-server/sse"
	"time"
)

func sseLogger() *slog.Logger {
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

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the DataStream (mock or real depending on your needs)
	mc := &DataStream{input: make(chan string)}

	//logger := sseLogger()

	// Create the SSE server
	server := sse.NewSSEServer(ctx, mc)

	// TODO include a http server inside the Event Server
	//TODO build a new listen and server which will start the run function of sse, handle mux routes, logging etc
	// and call listen and serve on the httpServer

	go server.Run()

	// Correct the handler path
	http.HandleFunc("/previous", server.HandleConnection)

	// Start the HTTP server
	err := http.ListenAndServe("localhost:8081", nil) // Consider using port 8080 for local testing
	if err != nil {
		log.Fatalf("Could not start server: %v", err)
	}
}
