package sse

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

//TODO use http test to create test server and mock request through the sse server

func TestSSEServer(t *testing.T) {

	testContext := context.Background()

	sse := NewSSEServer(testContext)

	go sse.Run()
	defer sse.cancel()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(sse.connection))

	testServer.Start()
	defer testServer.Close()

	err := MockRequest(t, &http.Client{}, testServer.URL, sse)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

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

	for {
		n, err := resp.Body.Read(buf)
		if err != nil {
			if err == io.EOF {
				break // End of stream
			}
			return fmt.Errorf("error reading from response body: %w", err)
		}

		message := buf[:n]
		t.Logf("Received message - sending to client:")

		// Send the message to the SSE broadcast channel
		select {
		case sseServer.broadcast <- message:
			// Successfully sent to broadcast channel
		case <-time.After(time.Second): // Add a timeout to avoid blocking indefinitely
			return fmt.Errorf("broadcast channel is full or blocked")
		}
	}

	return nil // Return nil if everything was successful
}

// MockHandler simulates a server that streams data to the client over time
func (sseServer *EventServer) connection(w http.ResponseWriter, r *http.Request) {
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

	message := make(chan []byte)
	sseServer.ConnectClient <- message

	for i := 0; i < 5; i++ {
		_, err := w.Write([]byte("Hello, world!\n"))
		if err != nil {
			return
		}

		// Flush the data immediately instead of buffering
		flusher.Flush()

		time.Sleep(1 * time.Second) // Simulate delay between messages
	}
}

//TODO implement redis like data stream mechanism to test

type DataStream struct {
	stream chan string
}

func MockStream(ds *DataStream) error {
	ds.stream = make(chan string, 5)
	testStrings := []string{
		"test 1", "test 2", "test 3", "test 4", "test 5",
	}
	go func() {
		for _, str := range testStrings {
			ds.stream <- str            // Send test string to the channel
			time.Sleep(1 * time.Second) // Simulate delay in data arrival
		}
	}()
	return nil
}

func MockSubscribe(t *testing.T, ctx context.Context, dataStream *DataStream, broadcast <-chan []byte) {

	return

}
