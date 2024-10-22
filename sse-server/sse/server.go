package sse

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

//TODO - look at improving the broadcast channel by potentially adding a buffer (dynamic?) or a custom queue mechanism
// to push into a buffer..?

// EventServer manages Server-Sent Events (SSE) by handling client connections and broadcasting messages.
// Each client is represented by a channel of byte slices in the 'clients' map.
// When a message is sent to the 'broadcast' channel, it is forwarded to all connected clients.
// Clients receive messages over their individual channels, and the server uses the HTTP connection
// (via http.ResponseWriter) to push data to them over the established SSE connection.
type EventServer struct {
	//broadcast     chan []byte
	context       context.Context
	cancel        context.CancelFunc
	ConnectClient chan chan []byte
	CloseClient   chan chan []byte
	clients       map[chan []byte]struct{} // Map to keep track of connected clients
	sync          sync.Mutex
	subscriber    RedisSubscriber
}

func NewSSEServer(parentCtx context.Context, rs RedisSubscriber) *EventServer {
	// Create a cancellable context derived from the parent context
	ctx, cancel := context.WithCancel(parentCtx)

	return &EventServer{
		//broadcast:     make(chan []byte),
		context:       ctx,
		cancel:        cancel, // Store the cancel function to stop the server later
		ConnectClient: make(chan chan []byte),
		CloseClient:   make(chan chan []byte),
		clients:       make(map[chan []byte]struct{}),
		subscriber:    rs,
	}
}

func (sseServer *EventServer) Run() {
	log.Println("Started server")
	for {
		select {
		case <-sseServer.context.Done():
			log.Println("Stopping SSE server")
			for client := range sseServer.clients {
				log.Println("Closing client: ", client)
				close(client)
				delete(sseServer.clients, client)
			}
			return

		case clientConnection := <-sseServer.ConnectClient:
			sseServer.sync.Lock()
			log.Println("Client connected")
			sseServer.clients[clientConnection] = struct{}{}
			sseServer.sync.Unlock()

			go sseServer.subscriber.RedisSubscriber(sseServer.context, "*", clientConnection)

		case clientDisconnect := <-sseServer.CloseClient:
			sseServer.sync.Lock()
			log.Println("Client disconnected")
			delete(sseServer.clients, clientDisconnect)
			sseServer.sync.Unlock()
		}
	}
}

func (sseServer *EventServer) Stop() {
	sseServer.cancel()
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

	message := make(chan []byte)
	sseServer.ConnectClient <- message

	//keeping the connection alive with keep-alive protocol
	keepAliveTicker := time.NewTicker(15 * time.Second)
	keepAliveMsg := []byte(":keepalive\n\n")
	notify := req.Context().Done()
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
				return
			}
			_, err := fmt.Fprintf(w, "data: %s\n\n", msg)
			log.Printf("broadcast: %s\n", msg)
			if err != nil {
				log.Printf("error writing messeage: %v", err)
				return
			}
			flusher.Flush()
		case <-keepAliveTicker.C:
			// Send the keep-alive ping
			_, err := w.Write(keepAliveMsg)
			if err != nil {
				log.Printf("error writing keepalive: %v", err)
				return
			}
			flusher.Flush()
		case <-serverNotify:
			log.Println("Beginning graceful shutdown")
			for msg := range message {
				// Handle remaining messages
				if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
					log.Printf("Error writing message: %v", err)
					return
				}
				flusher.Flush()
			}
			log.Println("No more messages - buffer cleared")
			return
		}
	}

}
