package sse

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

//TODO use http test to create test server and mock request through the sse server

func TestSSEServer(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mockRedis := &DataStream{input: make(chan string)}

	sse := NewSSEServer(testContext, mockRedis)

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
			err := MockRequest(t, &http.Client{}, testServer.URL, sse)
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

func MockRequest(t *testing.T, client *http.Client, url string, sseServer *EventServer) error {
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

//TODO implement redis like data stream mechanism to test

type DataStream struct {
	input chan string
}

// MockDataStream simulates subscribing to a data stream and forwarding messages to the SSE message channel.
// The 'ctx' represents the request-level context, and when it is done, the subscription and connection are terminated.
func (ds *DataStream) Subscribe(ctx context.Context, channel string, messageChan chan []byte) {
	log.Println("calling subscriber")
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
