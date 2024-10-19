package sse

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

//TODO use http test to create test server and mock request through the sse server

func TestSSEServer(t *testing.T) {

	testContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sse := NewSSEServer(testContext)

	go sse.Run()

	testServer := httptest.NewUnstartedServer(http.HandlerFunc(sse.connection))

	testServer.Start()
	defer testServer.Close()

	err := MockRequest(t, &http.Client{}, testServer.URL, sse)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

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

	//keeping the connection alive with keep-alive protocol
	keepAliveTicker := time.NewTicker(15 * time.Second)
	keepAliveMsg := []byte(":keepalive\n\n")
	notify := r.Context().Done()
	serverNotify := sseServer.context.Done()

	go func() {
		select {
		case <-serverNotify:
			log.Println("sse server context signalled done - closing connection")
			sseServer.CloseClient <- message
			keepAliveTicker.Stop()
		case <-notify:
			log.Println("request scope context signalled done - closing connection")
			sseServer.CloseClient <- message
			keepAliveTicker.Stop()
		}
	}()

	ds := &DataStream{
		input: make(chan string),
	}

	// Start the MockDataStream function in a separate goroutine
	go MockDataStream(sseServer.context, "test", ds, message)

	// Mock data stream to simulate sending events
	go func() {
		for i := 0; i < 5; i++ {
			// Send messages to the data stream input channel
			ds.input <- fmt.Sprintf("Test: %v", i) // Send plain string, MockDataStream will handle conversion
			time.Sleep(1 * time.Second)
		}
	}()

	// Loop to handle sending messages or keep-alive signals
	for {
		select {
		case msg, ok := <-message:
			if !ok {
				// Message channel closed, exit the loop
				log.Println("Message channel closed, exiting connection")
				return
			}
			// Write the message to the client
			_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
			if err != nil {
				log.Printf("Error writing message: %v", err)
				return
			}
			flusher.Flush() // Flush immediately to the client
		case <-keepAliveTicker.C:
			// Send the keep-alive ping
			_, err := w.Write(keepAliveMsg)
			if err != nil {
				log.Printf("Failed to send keep-alive: %v", err)
				return
			}
			flusher.Flush() // Ensure keep-alive is flushed immediately
		case <-serverNotify:
			log.Println("beginning graceful shutdown...")
			for {
				select {
				case msg, ok := <-message:
					if !ok {
						log.Println("Message channel empty")
						return
					}
					_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
					if err != nil {
						log.Printf("Error writing message: %v", err)
						return
					}
					flusher.Flush()
				default:
					log.Println("No more messages, buffer cleared")
					return
				}
			}
		}
	}

}

//TODO implement redis like data stream mechanism to test

type DataStream struct {
	input chan string
}

// MockDataStream simulates subscribing to a data stream and forwarding messages to the SSE message channel.
// The 'ctx' represents the request-level context, and when it is done, the subscription and connection are terminated.
func MockDataStream(ctx context.Context, channel string, ds *DataStream, messageChan chan []byte) {
	// Check if the channel is for testing purposes
	if channel == "test" {
		channel += ".*" // Adjust the channel string for mock purposes

		// Listen for incoming messages from the data stream
		go func() {
			for msg := range ds.input {
				select {
				case <-ctx.Done():
					// If the request context is done, break out of the loop
					return
				case messageChan <- []byte(msg):
					// Forward the message to the SSE message channel
				}
			}
		}()
	}

	// Wait for the request context to be done
	<-ctx.Done()

}
