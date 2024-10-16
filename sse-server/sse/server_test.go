package sse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

//TODO use http test to create test server and mock request through the sse server

func TestSSEServer(t *testing.T) {
	redisClient := &mockRedis{
		testMessages: []string{"Hello, world!", "Another message!", "Goodbye!"},
	}

	sseServer := NewSSEServer(redisClient)
	go sseServer.Start()

	// Create a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(sseServer.handleClientConnection))
	defer ts.Close()

	// Set a timeout for the HTTP client to avoid hanging indefinitely
	client := &http.Client{}

	// Make a request to the test server
	resp, err := client.Get(ts.URL + "/events/test")
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	defer resp.Body.Close()

	// Ensure we get a 200 OK response
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %v", resp.Status)
	}

	// Read messages from the response
	buf := make([]byte, 1024)

	for {
		n, err := resp.Body.Read(buf)
		if err != nil {
			if err == io.EOF {
				break // End of stream
			}
			t.Fatalf("Error reading from response body: %v", err)
		}

		t.Logf("Received message: %s", buf[:n])
	}

}

type mockRedis struct {
	testMessages []string
}

// RedisSubscriber simulates receiving messages from a Redis channel
func (m *mockRedis) RedisSubscriber(ctx context.Context, channel string, messageChan chan []byte) {
	if channel == "test.*" {
		for _, msg := range m.testMessages { // Iterate over predefined messages
			select {
			case <-ctx.Done():
				return // Exit if context is canceled
			case messageChan <- []byte(msg): // Send the message to the channel
				time.Sleep(1 * time.Second) // Simulate a delay to mimic message streaming
			}
		}
	}
}
