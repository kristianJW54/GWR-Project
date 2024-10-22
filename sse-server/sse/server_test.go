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
		t.Logf("Received message - sending to client:")
		t.Logf("%s", message)
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
func (ds *DataStream) RedisSubscriber(ctx context.Context, channel string, messageChan chan []byte) {
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
