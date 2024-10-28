package sse

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// TODO Check run function for main with different args
// TODO Check go-routine and no leakage
// TODO Check high load
// TODO BenchMark
// TODO Test with live services

func TestSSEServer(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mockRedis := &DataStream{Input: make(chan string)}

	// Initialize logger
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})

	logger := slog.New(logHandler)

	sse := NewSSEServer(testContext, mockRedis, logger)

	go sse.Run()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(sse.HandleConnection))
	testServer.Start()
	defer testServer.Close()

	// Number of simulated clients
	numClients := 5
	clientWaitGroup := sync.WaitGroup{}

	for i := 0; i < numClients; i++ {
		clientWaitGroup.Add(1)
		go func(clientID int) {
			defer clientWaitGroup.Done()
			err := MockRequest(t, &http.Client{}, testServer.URL)
			if err != nil {
				t.Errorf("Client %d: Failed to make request: %v", clientID, err)
			}
		}(i)
	}

	clientWaitGroup.Wait()

	// Test shutdown of SSE after the timeout
	<-testContext.Done()
	log.Println("Server closed successfully")
}

func TestSSELiveRedis(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Initialize logger
	logHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})

	logger := slog.New(logHandler)

	opt := &redis.Options{
		Addr:     "localhost:6379", // Use the service name instead of localhost
		Password: "",               // No password set
		DB:       0,
	}

	// Create a new Redis client
	redisClient, err := NewRedisClient(logger, opt) // Adjust the function signature if necessary
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}

	sse := NewSSEServer(testContext, redisClient, logger)

	// Start the SSE server in a goroutine
	go sse.Run()

	// Create an unstarted server for testing
	testServer := httptest.NewUnstartedServer(http.HandlerFunc(sse.HandleConnection))
	testServer.Start()
	defer testServer.Close()

	// Prepare the request to the SSE endpoint with Authorization header
	req, err := http.NewRequest(http.MethodGet, testServer.URL+"/admin/events/all", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "0800001066")

	// Make the request to the test server
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect to SSE server: %v", err)
	}
	defer resp.Body.Close()

	// Check for a successful response
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK; got %v", resp.Status)
	}

	// Wait for the context to expire
	<-testContext.Done()
	log.Println("Server closed successfully")
}

func MockRequest(t *testing.T, client *http.Client, url string) error {
	t.Helper()

	// Make the GET request
	resp, err := client.Get(url + "/events/test")
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close() // Ensure the body is closed after reading

	// Read messages from the response
	buf := make([]byte, 1024)
	//msg := make(chan []byte)

	for {
		n, err := resp.Body.Read(buf)
		if err != nil {
			if err == io.EOF {
				break // End of stream
			}
			return fmt.Errorf("error reading from response body: %w", err)
		}

		message := buf[:n]
		t.Logf("Received message - sending to client")
		t.Logf("%s", message)
	}

	return nil // Return nil if everything was successful
}
